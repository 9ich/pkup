package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

type IRCconn struct {
	Events    chan Event
	nick      string
	user      string
	real      string
	pass      string
	host      string
	err       chan error
	out       chan string
	msgtime   time.Time
	operators map[string]struct{}
	oplock    sync.Mutex
	conn      net.Conn
}

type Event struct {
	cmd  string
	msg  string
	nick string
	user string
	host string
	src  string
	args []string
	raw  string
}

const (
	Bold      = "\x02"
	Reset     = "\x0f\x02\x02"
	Underline = "\x15"
	White     = "\x0300"
	Black     = "\x0301"
	Dkblue    = "\x0302"
	Dkgreen   = "\x0303"
	Red       = "\x0304"
	Dkred     = "\x0305"
	Dkviolet  = "\x0306"
	Orange    = "\x0307"
	Yellow    = "\x0308"
	Ltgreen   = "\x0309"
	Cyan      = "\x0310"
	Ltcyan    = "\x0311"
	Blue      = "\x0312"
	Violet    = "\x0313"
	Dkgrey    = "\x0314"
	Ltgrey    = "\x0314"
)

var (
	quakecolours = regexp.MustCompile("\\^[0-9]")
	fmtcolours   = regexp.MustCompile("\\{[a-zA-Z]+\\}")
	spaces       = regexp.MustCompile("[ ]+")
)

var fmt2colours = map[string]string{
	"b":       Bold,
	"r":       Reset,
	"u":       Underline,
	"white":   White,
	"black":   Black,
	"dkblue":  Dkblue,
	"dkgreen": Dkgreen,
	"red":     Red,
	"dkred":   Dkred,
	"dkpink":  Dkviolet,
	"orange":  Orange,
	"yellow":  Yellow,
	"green":   Ltgreen,
	"cyan":    Cyan,
	"blue":    Blue,
	"pink":    Violet,
	"dkgrey":  Dkgrey,
	"grey":    Ltgrey,
}

func csprintf(format string, a ...interface{}) string {
	format = fmtcolours.ReplaceAllStringFunc(format, func(s string) string {
		str := strings.Trim(s, "{}")
		if c, ok := fmt2colours[str]; ok {
			return c
		}
		return s
	})
	return fmt.Sprintf(format, a...)
}

func colourconv(str string) string {
	str = quakecolours.ReplaceAllStringFunc(str, func(s string) string {
		switch s[1] {
		case '1':
			return Red
		case '2':
			return Ltgreen
		case '3':
			return Yellow
		case '4':
			return Blue
		case '5':
			return Cyan
		case '6':
			return Violet
		case '7':
			return White
		case '8':
			return Orange
		case '9':
			return Ltgrey
		default: // 0
			return Black
		}
	})
	return str + Reset
}

func newIRCconn(nick, user, real, pass string) *IRCconn {
	c := &IRCconn{
		nick:      nick,
		user:      user,
		real:      real,
		pass:      pass,
		operators: make(map[string]struct{}),
	}
	return c
}

func (c *IRCconn) dial(host string) error {
	conn, err := net.Dial("tcp", host)
	if err != nil {
		return err
	}
	c.Events = make(chan Event, 64)
	c.host = host
	c.conn = conn
	c.out = make(chan string, 64)
	c.err = make(chan error)
	c.msgtime = time.Now()
	go c.ping()
	go c.write()
	go c.read()
	log.Println("registering")
	c.out <- fmt.Sprintf("NICK %s", c.nick)
	c.out <- fmt.Sprintf("USER %s 0.0.0.0 0.0.0.0 :%s", c.user, c.real)
	if c.pass != "" {
		c.out <- fmt.Sprintf("PASS %s", c.pass)
	}
	return nil
}

func (c *IRCconn) Close() {
	close(c.out)
	close(c.err)
	close(c.Events)
	c.conn.Close()
}

func (c *IRCconn) privmsg(who, msg string) {
	c.out <- fmt.Sprintf("PRIVMSG %s :%s", who, msg)
}

func (c *IRCconn) notice(who, msg string) {
	c.out <- fmt.Sprintf("NOTICE %s :%s", who, msg)
}

func (c *IRCconn) topic(ch, topic string) {
	c.out <- fmt.Sprintf("TOPIC %s :%s", ch, topic)
}

func (c *IRCconn) join(ch string) {
	log.Println("joining", ch)
	c.out <- fmt.Sprintf("JOIN %s", ch)
}

func (c *IRCconn) part(ch string) {
	c.out <- fmt.Sprintf("PART %s", ch)
}

func (c *IRCconn) isopped(who, ch string) bool {
	c.oplock.Lock()
	defer c.oplock.Unlock()
	who, _, _ = splituserstring(who)
	_, ok := c.operators[who]
	return ok
}

func (c *IRCconn) ping() {
	tick := time.Tick(time.Minute)
	for {
		select {
		case <-tick:
			if time.Now().Sub(c.msgtime).Minutes() >= 3 {
				c.out <- fmt.Sprintf("PING %d", time.Now().UnixNano())
			}
		}
	}
}

// Listen to c.out and write to c.conn.
func (c *IRCconn) write() {
	for s := range c.out {
		if c.conn == nil {
			break
		}
		if _, err := c.conn.Write([]byte(s + "\r\n")); err != nil {
			log.Println(err)
			c.err <- err
			break
		}
	}
}

func (c *IRCconn) read() {
	scanner := bufio.NewScanner(c.conn)
	for scanner.Scan() {
		c.msgtime = time.Now()
		msg := scanner.Text()
		msg = spaces.ReplaceAllString(msg, " ")
		msg = strings.TrimSpace(msg)
		ev := Event{raw: msg}
		if msg[0] == ':' {
			// Extract full source.
			i := strings.Index(msg, " ")
			if i > -1 {
				ev.src = msg[1:i]
				msg = msg[i+1:]
			} else {
				log.Printf("? %s\n", msg)
			}
			// Extract nick!user@host.
			i = strings.Index(ev.src, "!")
			j := strings.Index(ev.src, "@")
			if i > -1 && j > -1 {
				ev.nick = ev.src[0:i]
				ev.user = ev.src[i+1 : j]
				ev.host = ev.src[j+1:]
			}
		}
		args := strings.SplitN(msg, " :", 2)
		if len(args) > 1 {
			ev.msg = args[1]
		}
		args = strings.Split(args[0], " ")
		ev.cmd = strings.ToLower(args[0])
		if len(args) > 1 {
			ev.args = args[1:]
		}
		c.handle(ev)
	}
}

// Numeric reply to command name
var num2cmd = map[string]string{
	"001": "welcome",
	"353": "rpl_namreply",
	"366": "rpl_endofnames",
}

func (c *IRCconn) handle(ev Event) {
	if ev.cmd == "privmsg" && ev.msg != "" && ev.msg[0] == '\x01' {
		// Turn CTCP queries into Events that are easier to handle generally.
		ev.cmd = strings.Trim(ev.msg, "\x01")
		ev.cmd = strings.ToLower(ev.cmd)
		if ev.cmd[0:4] == "ping" {
			ev.cmd = "ctcp-ping"
		}
	}
	if cmd, ok := num2cmd[ev.cmd[0:3]]; ok {
		ev.cmd = cmd
	}
	switch ev.cmd {
	case "welcome":
		log.Println("received welcome message")
		c.out <- fmt.Sprintf("NAMES %s", channel)
		c.Events <- ev
	case "rpl_namreply":
		c.processnames(ev.msg)
	case "mode":
		c.processmode(ev.args)
	case "nick":
		c.processnick(ev.nick, ev.msg)
	case "part":
		fallthrough
	case "quit":
		c.processquit(ev.nick)
	case "ping":
		c.out <- fmt.Sprintf("PONG :%s", ev.msg)
	case "version":
		c.out <- fmt.Sprintf("NOTICE %s :\x01VERSION %s\x01", ev.nick, version)
	case "time":
		c.out <- fmt.Sprintf("NOTICE %s :\x01TIME %s\x01", ev.nick, time.Now().Local().String())
	case "ctcp-ping":
		c.out <- fmt.Sprintf("NOTICE %s :%s", ev.nick, ev.msg)
	case "finger":
		c.out <- fmt.Sprintf("NOTICE %s :\x01l-lewd!\x01", ev.nick)
	default:
		c.Events <- ev
	}
}

func (c *IRCconn) processnames(msg string) {
	c.oplock.Lock()
	defer c.oplock.Unlock()
	for _, s := range strings.Split(msg, " ") {
		if len(s) < 2 {
			continue
		}
		if s[0] == '@' {
			c.operators[s[1:]] = struct{}{}
		} else {
			if s[0] == '+' {
				s = s[1:]
			}
			delete(c.operators, s)
		}
	}
}

func (c *IRCconn) processmode(args []string) {
	c.oplock.Lock()
	defer c.oplock.Unlock()
	args = args[1:]
	for len(args) >= 2 {
		if args[0] == "+o" {
			c.operators[args[1]] = struct{}{}
		} else if args[0] == "-o" {
			delete(c.operators, args[1])
		}
		args = args[2:]
	}
}

func (c *IRCconn) processnick(who, to string) {
	c.oplock.Lock()
	defer c.oplock.Unlock()
	if _, ok := c.operators[who]; !ok {
		return
	}
	delete(c.operators, who)
	c.operators[to] = struct{}{}
}

func (c *IRCconn) processquit(who string) {
	delete(c.operators, who)
}

// "nick!user@host" to "nick", "user", "host"
func splituserstring(s string) (string, string, string) {
	i := strings.Index(s, "!")
	j := strings.Index(s, "@")
	if i > -1 && j > -1 {
		return s[0:i], s[i+1 : j], s[j+1:]
	}
	return "", "", ""
}
