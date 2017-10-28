package main

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

type QServer struct {
	kv       map[string]string
	qalias   string
	qhost    string
	qport    string
	qpass    string
	qclients []Client
	qping    time.Duration
	qonline  bool
}

func newqserver(alias, host, pass string) Server {
	srv := &QServer{
		kv:     make(map[string]string, 16),
		qalias: alias, qpass: pass,
	}
	split := strings.Split(host, ":")
	srv.qhost = split[0]
	if len(split) < 2 {
		srv.qport = "27960"
	} else {
		srv.qport = split[1]
	}
	return srv
}

func (srv *QServer) query() error {
	srv.qonline = false
	host := fmt.Sprintf("%s:%s", srv.qhost, srv.qport)
	c, err := net.DialTimeout("udp", host, 1*time.Second)
	if err != nil {
		return err
	}
	c.SetDeadline(time.Now().Add(1 * time.Second))
	defer c.Close()
	b := []byte("\xFF\xFF\xFF\xFFgetstatus\n")
	r := make([]byte, 2048)
	t := time.Now()
	if _, err := c.Write(b); err != nil {
		return err
	}
	n, err := c.Read(r)
	if err != nil {
		return err
	}
	srv.qping = time.Now().Sub(t)
	want := []byte("\xFF\xFF\xFF\xFFstatusResponse\n")
	if n < len(want) || !bytes.Equal(r[:len(want)], want) {
		return errors.New("bad response")
	}

	r = r[len(want):n]
	data := strings.Split(string(r), "\n")
	if len(data) < 1 {
		return errors.New("bad response")
	}
	// Key-value pairs.
	kvs := strings.Split(data[0], "\\")
	for i := range kvs {
		if len(kvs)-i < 2 {
			break
		}
		srv.kv[strings.ToLower(kvs[i])] = kvs[i+1]
	}
	if len(data) < 2 {
		return nil
	}
	// Client info.
	srv.qclients = make([]Client, 0)
	for _, s := range data[1:] {
		ss := strings.Split(s, " ")
		if len(ss) < 3 {
			continue
		}
		nick := strings.Trim(ss[2], "\"")
		score, _ := strconv.Atoi(ss[0])
		cl := Client{name: nick, score: score}
		srv.qclients = append(srv.qclients, cl)
	}
	srv.qonline = true
	return nil
}

func (srv *QServer) alias() string {
	return srv.qalias
}

func (srv *QServer) host() string {
	return srv.qhost
}

func (srv *QServer) hostname() string {
	return colourconv(srv.kv["sv_hostname"])
}

func (srv *QServer) password() string {
	return srv.qpass
}

func (srv *QServer) clients() Clients {
	return srv.qclients
}

func (srv *QServer) maxclients() int {
	n, _ := strconv.Atoi(srv.kv["sv_maxclients"])
	return n
}

func (srv *QServer) mapname() string {
	return srv.kv["mapname"]
}

func (srv *QServer) gametype() string {
	var gametypes = map[string]string{
		// CPMA/Quake
		"-1": "hoonymode",
		"0":  "ffa",
		"1":  "1v1",
		"3":  "tdm",
		"4":  "ctf",
		"5":  "va",
		"6":  "freeze",
		"7":  "ctfs",
		"8":  "ntf",
	}
	if t, ok := gametypes[srv.kv["g_gametype"]]; ok {
		return t
	}
	// This is fine for Warsow.
	if t, ok := srv.kv["g_gametype"]; ok {
		return t
	}
	if t, ok := srv.kv["gametype"]; ok {
		return t
	}
	return "Unknown gametype"
}

func (srv *QServer) timelimit() int {
	n, _ := strconv.Atoi(srv.kv["timelimit"])
	return n
}

func (srv *QServer) fraglimit() int {
	n, _ := strconv.Atoi(srv.kv["fraglimit"])
	return n
}

func (srv *QServer) capturelimit() int {
	n, _ := strconv.Atoi(srv.kv["capturelimit"])
	return n
}

func (srv *QServer) ping() time.Duration {
	return srv.qping
}

func (srv *QServer) port() string {
	return srv.qport
}

func (srv *QServer) online() bool {
	return srv.qonline
}
