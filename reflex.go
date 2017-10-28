package main

import (
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"
)

type ReflexServer struct {
	ralias      string
	rhost       string
	rport       string
	steamport   string
	rhostname   string
	rpass       string
	rmapname    string
	folder      string
	game        string
	id          int16
	rmaxclients byte
	nclients    byte
	rclients    []Client
	rping       time.Duration
	ronline     bool
	// Extra fields.
	tv string // SourceTV addr+port
}

var (
	ErrShort       = errors.New("unexpected end of data")
	ErrShortInfo   = errors.New("short info response")
	ErrShortExtra  = errors.New("short extra info data")
	ErrShortPlayer = errors.New("short player response")
	ErrBad         = errors.New("bad response")
)

//	s	string
//	b	byte
//	S	int16
//	l	int32
//	L	uint64
//	f	float32
func unpack(format string, b []byte, a ...interface{}) ([]byte, error) {
	if len(format) != len(a) {
		return b, errors.New("unpack: bad format string or args")
	}
	for i := 0; i < len(format); i++ {
		switch format[i] {
		case 's':
			s, ok := a[i].(*string)
			if !ok {
				return b, errors.New("unpack: expected *string")
			}
			*s = ""
			if len(b) < 1 {
				return b, ErrShort
			}
			var k int
			for k = 0; k < len(b) && b[k] != 0; k++ {
				*s += string(b[k])
			}
			if b[k] != 0 {
				return b, ErrShort
			}
			b = b[k+1:]
		case 'b':
			bb, ok := a[i].(*byte)
			if !ok {
				return b, errors.New("unpack: expected *byte")
			}
			if len(b) < 1 {
				return b, ErrShort
			}
			*bb = b[0]
			b = b[1:]
		case 'S':
			n, ok := a[i].(*int16)
			if !ok {
				return b, errors.New("unpack: expected *int16")
			}
			if len(b) < 2 {
				return b, ErrShort
			}
			*n = int16(b[0]) | (int16(b[1]) << 8)
			b = b[2:]
		case 'l':
			n, ok := a[i].(*int32)
			if !ok {
				return b, errors.New("unpack: expected *int32")
			}
			if len(b) < 4 {
				return b, ErrShort
			}
			*n = int32(b[0]) | (int32(b[1]) << 8) | (int32(b[2]) << 16) | (int32(b[3]) << 24)
			b = b[4:]
		case 'L':
			n, ok := a[i].(*uint64)
			if !ok {
				return b, errors.New("unpack: expected *uint64")
			}
			if len(b) < 8 {
				return b, ErrShort
			}
			*n = uint64(b[0]) | (uint64(b[1]) << 8) | (uint64(b[2]) << 16) |
				(uint64(b[3]) << 24) | (uint64(b[4]) << 32) | (uint64(b[5]) << 40) |
				(uint64(b[6]) << 48) | (uint64(b[7]) << 56)
			b = b[8:]
		case 'f':
			n, ok := a[i].(*float32)
			if !ok {
				return b, errors.New("unpack: expected *float32")
			}
			if len(b) < 4 {
				return b, ErrShort
			}
			i := uint32(b[0]) | (uint32(b[1]) << 8) | (uint32(b[2]) << 16) | (uint32(b[3]) << 24)
			*n = math.Float32frombits(i)
			b = b[4:]
		}
	}
	return b, nil
}

func unpackEDF(b []byte, port *int16, tv *string) ([]byte, error) {
	if len(b) < 1 {
		return b, errors.New("unpack: short extra data")
	}
	edf := b[0]
	b = b[1:]
	var err error
	if edf&0x80 != 0 { // port
		if b, err = unpack("S", b, port); err != nil {
			return b, err
		}
	}
	if edf&0x10 != 0 { // SteamID
		if len(b) < 8 {
			return b, ErrShortExtra
		}
		b = b[8:]
	}
	if edf&0x40 != 0 { // SourceTV
		var tvport int16
		var tvsrv string
		if b, err = unpack("Ss", b, &tvport, &tvsrv); err != nil {
			return b, err
		}
		*tv = tvsrv + ":" + strconv.Itoa(int(tvport))
	}
	return b, nil
}

func unpackclients(b []byte) ([]Client, error) {
	if len(b) < 6 {
		return nil, ErrShortPlayer
	}
	if b[4] != 0x44 {
		return nil, ErrBad
	}
	nclients := int(b[5])
	b = b[6:]
	cs := make([]Client, nclients)
	var err error
	for i := 0; i < nclients; i++ {
		var idx byte
		var score int32
		var dur float32
		b, err = unpack("bslf", b, &idx, &cs[i].name, &score, &dur)
		if err != nil {
			return cs, err
		}
		cs[i].score = int(score)
	}
	return cs, nil
}

var ports = []string{
	"25797",
	"25798",
	"25799",
	"25800",
	"25801",
	"25803",
	"25805",
	"25807",
}

func newreflexserver(alias, host, pass string) Server {
	srv := &ReflexServer{ralias: alias, rpass: pass}
	srv.steamport = "25797"
	split := strings.Split(host, ":")
	srv.rhost = split[0]
	if len(split) > 1 {
		srv.steamport = split[1]
	}
	return srv
}

func (srv *ReflexServer) query() error {
	srv.ronline = false
	var c net.Conn
	var err error
	ports[0] = srv.steamport
	for i := range ports {
		host := fmt.Sprintf("%s:%s", srv.rhost, ports[i])
		c, err = net.DialTimeout("udp", host, 1*time.Second)
		if err == nil {
			break
		}
	}
	if err != nil {
		return err
	}
	c.SetDeadline(time.Now().Add(1 * time.Second))
	defer c.Close()

	// Info query.
	b := []byte("\xFF\xFF\xFF\xFFTSource Engine Query\x00")
	r := make([]byte, 8192)
	rr := r
	t := time.Now()
	if _, err := c.Write(b); err != nil {
		return err
	}
	n, err := c.Read(r)
	if err != nil {
		return err
	}
	srv.rping = time.Now().Sub(t)
	if len(r) <= 6 {
		return errors.New("short info response")
	}
	var dum byte
	var version string
	r, err = unpack("ssssSbbbbbbbs", r[6:n], &srv.rhostname,
		&srv.rmapname, &srv.folder, &srv.game, &srv.id,
		&srv.nclients, &srv.rmaxclients, &dum, &dum, &dum,
		&dum, &dum, &version)
	if err != nil {
		return err
	}
	var port int16
	r, err = unpackEDF(r, &port, &srv.tv)
	srv.rport = strconv.Itoa(int(port))
	srv.ronline = true
	r = rr

	// Player query.
	// Challenge request.
	b = []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x55, 0xFF, 0xFF, 0xFF, 0xFF}
	if _, err := c.Write(b); err != nil {
		return err
	}
	if n, err = c.Read(r); err != nil {
		return err
	}
	if n < 9 {
		return ErrShort
	}
	// Send the challenge back.
	copy(b[5:9], r[5:9])
	if _, err := c.Write(b); err != nil {
		return err
	}
	if n, err = c.Read(r); err != nil {
		return err
	}
	if srv.rclients, err = unpackclients(r[:n]); err != nil {
		return err
	}
	return nil
}

func (srv *ReflexServer) alias() string {
	return srv.ralias
}

func (srv *ReflexServer) host() string {
	return srv.rhost
}

func (srv *ReflexServer) hostname() string {
	return srv.rhostname
}

func (srv *ReflexServer) password() string {
	return srv.rpass
}

func (srv *ReflexServer) clients() Clients {
	return srv.rclients
}

func (srv *ReflexServer) maxclients() int {
	return int(srv.rmaxclients)
}

func (srv *ReflexServer) mapname() string {
	return srv.rmapname
}

func (srv *ReflexServer) gametype() string {
	return "(Reflex)"
}

func (srv *ReflexServer) timelimit() int {
	return 0
}

func (srv *ReflexServer) fraglimit() int {
	return 0
}

func (srv *ReflexServer) capturelimit() int {
	return 0
}

func (srv *ReflexServer) ping() time.Duration {
	return srv.rping
}

func (srv *ReflexServer) port() string {
	return srv.rport
}

func (srv *ReflexServer) online() bool {
	return srv.ronline
}
