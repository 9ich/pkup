package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	name  string
	score int
}

type Clients []Client

type Server interface {
	query() error
	alias() string
	hostname() string // e.g. "#CPMPICKUP #1 - Roboty Arena"
	host() string     // e.g. "cpmpickup.de"
	port() string
	password() string
	clients() Clients
	maxclients() int
	mapname() string
	gametype() string
	timelimit() int
	fraglimit() int
	capturelimit() int
	ping() time.Duration
	online() bool
}

type Mode struct {
	name       string
	srvs       []Server // Server pool.
	srv        Server   // Last chosen server.
	who        []Player // Players added.
	nneeded    int      // Players needed.
	cap1, cap2 string   // Captains.
}

type Modes []*Mode // sort.Interface

// An !added user.
type Player struct {
	user   string    // user@host
	expire time.Time // Expiry time.
}

type HistVal struct {
	t    time.Time
	mode string
	nick string
}

type Top10Val struct {
	name string
	n    int
}

type Top10 []Top10Val // sort.Interface

type Botfn struct {
	fn   func(string, string, ...string) (ok bool) // The command.
	save bool                                      // Save to pickup.rc?
	op   bool                                      // Operators only?
}

const (
	runcommands   = "pickup.rc"
	histfile      = "pickuphistory.log"
	tlayout       = "2006-01-02 15:04"
	defaultexpire = "3h"
)

const ErrPermission = "only ops may use that command"

var (
	nick      = flag.String("n", "pickupbot`", "nickname")
	user      = flag.String("u", "pkup", "the 'user' part of 'user@host'")
	real      = flag.String("r", Violet+"pickupbot", "real name, can contain spaces")
	vol       = flag.Int("v", 2, "intrusiveness of command responses; 1=message user, 2=notice user, 3=message channel, 4=notice channel")
	ccflag    = flag.String("cc", "", "other channels to send !promote and !sub messages to, comma-separated")
	ccto      []string
	host      string
	channel   string
	modes     = make(map[string]*Mode)
	motd      string
	mumble    string
	teamspeak string
	voip      string
	irc       *IRCconn
	lastgame  *Mode
	initial   = true
	version   = "pkup"
)

var botcmds = map[string]Botfn{
	"add":       {add, false, false},
	"addserver": {addserver, true, true},
	"delmode":   {delmode, true, true},
	"delserver": {delserver, true, true},
	"expire":    {setexpire, false, false},
	"help":      {help, false, false},
	"lastgame":  {showlastgame, false, false},
	"list":      {listservers, false, false},
	"mode":      {addmode, true, true},
	"modes":     {listmodes, false, false},
	"month":     {top10month, false, false},
	"motd":      {setmotd, true, true},
	"mumble":    {querymumble, false, false},
	"promote":   {promote, false, false},
	"q":         {serverinfo, false, false},
	"remove":    {remove, false, false},
	"setmumble": {setmumble, true, true},
	"setts":     {setts, true, true},
	"setvoip":   {setvoip, true, true},
	"top":       {topmost, false, false},
	"top10":     {top10players, false, false},
	"top25":     {top25players, false, false},
	"ts":        {queryts, false, false},
	"version":   {showversion, false, false},
	"voip":      {queryvoip, false, false},
	"week":      {top10week, false, false},
	"who":       {listplayers, false, false},
}

func say(where, who, what string) {
	what = Violet + what
	who, _, _ = splituserstring(who)
	switch *vol {
	case 1:
		irc.privmsg(who, what)
	case 2:
		irc.notice(who, what)
	case 3:
		irc.privmsg(where, what)
	default: // 4
		irc.notice(where, what)
	}
}

func sayusage(where, who, what string) {
	what = Violet + what
	who, _, _ = splituserstring(who)
	irc.notice(who, what)
}

func addmode(where, who string, args ...string) bool {
	usage := "usage: !mode name numplayers"
	if len(args) != 2 {
		if initial {
			log.Println(usage)
		} else {
			sayusage(where, who, usage)
		}
		return false
	}
	n, err := strconv.Atoi(args[1])
	if err != nil {
		if initial {
			log.Println(usage)
		} else {
			sayusage(where, who, usage)
		}
		return false
	}

	// Create the mode if it doesn't exist.
	k := strings.ToLower(args[0])
	if _, ok := modes[k]; !ok {
		modes[k] = &Mode{name: args[0]}
	}
	modes[k].nneeded = n
	if !initial {
		updatetopic()
		if len(modes[k].who) >= modes[k].nneeded {
			modes[k].startgame()
		}
	}
	return true
}

func delmode(where, who string, args ...string) bool {
	if len(args) != 1 {
		sayusage(where, who, "usage: !delmode name")
		return false
	}
	k := strings.ToLower(args[0])
	delete(modes, k)
	updatetopic()
	return true
}

func addserver(where, who string, args ...string) bool {
	if len(args) < 4 {
		s := "usage: !addserver alias host[:port][;password] game mode1 mode2 ..."
		if initial {
			log.Println(s)
		} else {
			sayusage(where, who, s)
			sayusage(where, who, "games: q3, reflex, cpm, warsow")
		}
		return false
	}

	alias := strings.ToLower(args[0])
	s := strings.SplitN(args[1], ";", 2)
	host := s[0]
	pass := ""
	if len(s) > 1 {
		pass = s[1]
	}
	game := args[2]
	for _, mode := range args[3:] {
		k := strings.ToLower(mode)
		// Create the mode.
		if _, ok := modes[k]; !ok {
			modes[k] = &Mode{name: mode, nneeded: 1}
		}
		m := modes[k]
		srv, err := newserver(game, args[0], host, pass)
		if err != nil {
			if initial {
				log.Println(err)
			} else {
				sayusage(where, who, err.Error())
			}
			return false
		}
		for i := range m.srvs {
			if strings.ToLower(m.srvs[i].alias()) == alias {
				m.srvs[i] = srv
				return true
			}
		}
		m.srvs = append(m.srvs, srv)
	}
	if !initial {
		updatetopic()
	}
	return true
}

func delserver(where, who string, args ...string) bool {
	if len(args) != 1 {
		sayusage(where, who, "usage: !delserver alias")
		return false
	}

	alias := strings.ToLower(args[0])
Search:
	for _, m := range modes {
		for i := range m.srvs {
			if strings.ToLower(m.srvs[i].alias()) == alias {
				m.srvs[i], m.srvs[len(m.srvs)-1], m.srvs =
					m.srvs[len(m.srvs)-1], nil, m.srvs[:len(m.srvs)-1]
				continue Search
			}
		}
	}
	return true
}

func add(where, who string, args ...string) bool {
	update := false
	defer func() {
		if update {
			updatetopic()
		}
		for _, m := range modes {
			if len(m.who) >= m.nneeded {
				m.startgame()
				break
			}
		}
	}()
	if len(args) < 1 {
		for _, m := range modes {
			u := m.addplayer(who)
			update = u || update
			m.srv = nil
		}
		return true
	}
	for _, mname := range args {
		// Exclude add: "-ctf" means to add to every mode except "ctf"
		mname = strings.ToLower(mname)
		if mname[0] == '-' && len(mname) > 1 {
			for k, m := range modes {
				if k != mname[1:] {
					u := m.addplayer(who)
					update = u || update
				} else {
					u := m.removeplayer(who)
					update = u || update
				}
			}
			continue
		}
		// Normal add.
		m, ok := modes[mname]
		if !ok {
			say(where, who, fmt.Sprintf("%s: no such mode", mname))
			return false
		}
		u := m.addplayer(who)
		update = u || update
		m.srv = nil
	}
	return true
}

func remove(where, who string, args ...string) bool {
	ms := make([]*Mode, 0, len(modes))
	if len(args) < 1 {
		for _, m := range modes {
			ms = append(ms, m)
		}
	} else {
		for _, mname := range args {
			mname = strings.ToLower(mname)
			if m, ok := modes[mname]; ok {
				ms = append(ms, m)
			}
		}
	}
	update := false
	for _, m := range ms {
		u := m.removeplayer(who)
		update = u || update
	}
	if update {
		updatetopic()
	}
	return true
}

func setexpire(where, who string, args ...string) bool {
	if len(args) < 1 {
		sayusage(where, who, "usage: !expire 1m30s")
		return false
	}

	now := time.Now()
	expire, err := time.ParseDuration(args[0])
	if err != nil {
		sayusage(where, who, "error: bad time string")
		return false
	}

	for i, m := range modes {
		for j, u := range m.who {
			if u.user == who {
				modes[i].who[j].expire = now.Add(expire)
			}
		}
	}
	return true
}

func setmumble(where, who string, args ...string) bool {
	if len(args) < 1 {
		sayusage(where, who, "usage: !setmumble addr")
		return false
	}
	mumble = args[0]
	return true
}

func setts(where, who string, args ...string) bool {
	if len(args) < 1 {
		sayusage(where, who, "usage: !setts addr")
		return false
	}
	teamspeak = args[0]
	return true
}

func setvoip(where, who string, args ...string) bool {
	if len(args) < 1 {
		sayusage(where, who, "usage: !setvoip addr")
		return false
	}
	voip = args[0]
	return true
}

func listplayers(where, who string, args ...string) bool {
	who, _, _ = splituserstring(who)
	if len(args) < 1 {
		for _, m := range modes {
			nicks := ""
			for _, u := range m.who {
				nick, _, _ := splituserstring(u.user)
				nicks += nick + " "
			}
			nicks = strings.Trim(nicks, " ")
			if nicks != "" {
				say(where, who, fmt.Sprintf("%s: %s", m.name, nicks))
			}
		}
	}
	for _, mname := range args {
		mname = strings.ToLower(mname)
		m, ok := modes[mname]
		if !ok {
			say(where, who, fmt.Sprintf("%s: no such mode", mname))
			return false
		}
		nicks := ""
		for _, u := range m.who {
			nick, _, _ := splituserstring(u.user)
			nicks += nick + " "
		}
		nicks = strings.Trim(nicks, " ")
		say(where, who, fmt.Sprintf("%s: %s", m.name, nicks))
	}
	return true
}

func serverinfo(where, who string, args ...string) bool {
	if len(args) != 1 {
		sayusage(where, who, "usage: !q alias")
		return false
	}
	var srv Server
Look:
	for i := range modes {
		for _, s := range modes[i].srvs {
			if s.alias() == args[0] {
				srv = s
				break Look
			}
		}
	}
	if srv == nil {
		sayusage(where, who, "no such server")
		return false
	}
	if srv.query() != nil {
		s := csprintf("{pink}{b}%s{b} is {green}{b}%s:%s{b} {r}and is not responding",
			srv.alias(), srv.host(), srv.port())
		irc.notice(where, s)
		return false
	}
	pass := ""
	if srv.password() != "" {
		pass = fmt.Sprintf(";password %s", srv.password())
	}
	s1 := csprintf("{pink}{b}%s{b} is {green}{b}%s:%s%s{b} {r}[{cyan}%s{r}]",
		srv.alias(), srv.host(), srv.port(), pass, srv.hostname())
	s2 := csprintf("{b}%s{b} on {green}{b}%s{b} {cyan}(%d/%d){r} || Players: {cyan}[{r}%v{cyan}]{r}",
		srv.gametype(), srv.mapname(), len(srv.clients()), srv.maxclients(), srv.clients())
	irc.notice(where, s1)
	irc.notice(where, s2)
	return true
}

func help(where, who string, args ...string) bool {
	cmds := []string{
		"add",
		"expire",
		"help",
		"lastgame",
		"list",
		"modes",
		"month",
		"mumble",
		"promote",
		"q",
		"remove",
		"ts",
		"top",
		"top10",
		"top25",
		"version",
		"week",
		"who",
	}
	opcmds := []string{
		"addserver",
		"delmode",
		"delserver",
		"mode",
		"motd",
		"setmumble",
		"setts",
		"setvoip",
	}
	s := "commands:"
	for i := range cmds {
		s += fmt.Sprintf(" !%s", cmds[i])
	}
	say(where, who, s)
	if irc.isopped(who, channel) {
		s = "op commands:"
		for i := range opcmds {
			s += fmt.Sprintf(" !%s", opcmds[i])
		}
		say(where, who, s)
	}
	return true
}

func showlastgame(where, who string, args ...string) bool {
	if lastgame == nil {
		say(where, who, "none")
		return false
	}
	m := lastgame
	nicks := make([]string, 0)
	for _, u := range m.who {
		nick, _, _ := splituserstring(u.user)
		nicks = append(nicks, nick)
	}
	nicksstr := strings.Join(nicks, " ")
	srvstr := ""
	captainsstr := ""
	if m.srv == nil {
		srvstr = Violet + "but there are no online servers in its pool =["
	} else {
		host := fmt.Sprintf("%s:%s", m.srv.host(), m.srv.port())
		if m.srv.password() != "" {
			srvstr = csprintf("{pink}{b}connect %s;password %s",
				host, m.srv.password())
		} else {
			srvstr = csprintf("{pink}{b}connect %s", host)
		}
	}
	if m.teamgame() && len(m.who) >= 2 {
		captainsstr = csprintf("{r} || team captains are {red}%s{r} and {blue}%s{r}",
			m.cap1, m.cap2)
	}
	s := csprintf("{orange}{b}%s{b} is ready {r}-> %s {r}<- {orange}%s%s",
		m.name, srvstr, nicksstr, captainsstr)
	say(where, who, s)
	return true
}

func promote(where, who string, args ...string) bool {
	var m *Mode
	if len(args) > 0 {
		m = modes[strings.ToLower(args[0])]
		if m == nil {
			sayusage(where, who, fmt.Sprintf("%s: no such mode", args[0]))
			return false
		}
	} else {
		for _, mm := range modes {
			if m == nil || len(mm.who) > len(m.who) {
				m = mm
			}
		}
	}
	if len(m.who) >= m.nneeded {
		return false
	}
	cc := append([]string{channel}, ccto...)
	for i := range cc {
		s := csprintf("{pink}Please !add for {b}%s{b} {cyan}[%d/%d]{pink} in {b}%s{b}!",
			m.name, len(m.who), m.nneeded, channel)
		irc.notice(cc[i], s)
		time.Sleep(60 * time.Millisecond)
	}
	return true
}

func listmodes(where, who string, args ...string) bool {
	s := ""
	for k, _ := range modes {
		s += " " + k
	}
	say(where, who, s)
	return true
}

func listservers(where, who string, args ...string) bool {
	for _, m := range modes {
		for _, srv := range m.srvs {
			if srv.password() != "" {
				say(where, who, fmt.Sprintf("%s: %s is %s;password %s",
					m.name, srv.alias(), srv.host(), srv.password()))
			} else {
				say(where, who, fmt.Sprintf("%s: %s is %s",
					m.name, srv.alias(), srv.host()))
			}
			time.Sleep(60 * time.Millisecond)
		}
	}
	return true
}

func setmotd(where, who string, args ...string) bool {
	if len(args) < 1 {
		sayusage(where, who, "usage: !motd text")
		return false
	}
	motd = ""
	for _, s := range args {
		motd += s + " "
	}
	updatetopic()
	return true
}

func querymumble(where, who string, args ...string) bool {
	say(where, who, csprintf("{b}mumble{b}: %s", mumble))
	return true
}

func queryts(where, who string, args ...string) bool {
	say(where, who, csprintf("{b}ts{b}: %s", teamspeak))
	return true
}

func queryvoip(where, who string, args ...string) bool {
	say(where, who, csprintf("{b}voip{b}: %s", voip))
	return true
}

func (ms Modes) Len() int      { return len(ms) }
func (ms Modes) Swap(i, j int) { ms[i], ms[j] = ms[j], ms[i] }
func (ms Modes) Less(i, j int) bool {
	return strings.ToLower(ms[i].name) < strings.ToLower(ms[j].name)
}

func updatetopic() {
	ms := make([]string, 0, len(modes))
	sorted := make(Modes, len(modes))
	i := 0
	for k := range modes {
		sorted[i] = modes[k]
		i++
	}
	sort.Sort(sorted)

	for _, m := range sorted {
		s := csprintf("{red}{b}%s{b} {dkred}[%d/%d]{r}", m.name, len(m.who), m.nneeded)
		ms = append(ms, s)
	}
	if motd != "" {
		ms = append(ms, motd)
	}
	sep := csprintf("{blue} ][ {r}")
	tpc := strings.Join(ms, sep)
	irc.topic(channel, tpc)
}

func newserver(game, alias, host, pass string) (Server, error) {
	switch game {
	case "q3", "quake", "cpm", "cpma", "wsw", "warsow":
		return newqserver(alias, host, pass), nil
	case "reflex":
		return newreflexserver(alias, host, pass), nil
	default:
		return nil, errors.New("bad game name")
	}
}

func (c Client) String() string {
	name := colourconv(c.name)
	return fmt.Sprintf("%s:%d", name, c.score)
}

func (m *Mode) clone() *Mode {
	c := *m
	c.srvs = make([]Server, len(m.srvs))
	c.who = make([]Player, len(m.who))
	copy(c.srvs, m.srvs)
	copy(c.who, m.who)
	return &c
}

func (m *Mode) teamgame() bool {
	ms := []string{"tdm", "ctf", "ntf", "ca", "3v3", "bomb"}
	s := strings.ToLower(m.name)
	for i := range ms {
		if strings.Contains(s, ms[i]) {
			return true
		}
	}
	return false
}

func (m *Mode) addplayer(who string) bool {
	// Already added?
	for _, u := range m.who {
		if u.user == who {
			return false
		}
	}
	now := time.Now()
	expire, _ := time.ParseDuration(defaultexpire)
	m.who = append(m.who, Player{who, now.Add(expire)})
	return true
}

func (m *Mode) removeplayer(who string) bool {
	removed := false
	for i, u := range m.who {
		if u.user == who {
			copy(m.who[i:], m.who[i+1:])
			m.who[len(m.who)-1] = Player{}
			m.who = m.who[:len(m.who)-1]
			removed = true
		}
	}
	return removed
}

func (m *Mode) startgame() {
	m.updateservers()
	m.pickcaptains()
	m.promotestarting()
	who := make([]Player, len(m.who))
	copy(who, m.who)
	loggamestart(m.name, who)
	lastgame = m.clone()
	for _, m := range modes {
		for _, u := range who {
			m.removeplayer(u.user)
		}
	}
	m.who = []Player{}
	go func() {
		time.Sleep(5 * time.Second)
		updatetopic()
	}()
}

func (m *Mode) updateservers() {
	m.srv = nil
	for i := range m.srvs {
		if err := m.srvs[i].query(); err != nil {
			log.Println(err)
		}
	}
	// Choose the server for this game.
	for i, srv := range m.srvs {
		if !srv.online() {
			continue
		}
		if m.srv == nil {
			m.srv = m.srvs[i]
			continue
		}
		// Prefer lower ping and emptier servers.
		if srv.ping() < m.srv.ping() || len(srv.clients()) < len(m.srv.clients()) {
			m.srv = m.srvs[i]
		}
	}
}

func (m *Mode) pickcaptains() {
	if m.teamgame() && len(m.who) >= 2 {
		leng := len(m.who) / 2
		i, j := rand.Intn(leng), rand.Intn(leng)+leng
		if i > 0 && len(m.who)%2 != 0 {
			i--
		}
		m.cap1, m.cap2 = m.who[i].user, m.who[j].user
		m.cap1, _, _ = splituserstring(m.cap1)
		m.cap2, _, _ = splituserstring(m.cap2)
	}
}

func (m *Mode) promotestarting() {
	nicks := make([]string, 0)
	for _, u := range m.who {
		nick, _, _ := splituserstring(u.user)
		nicks = append(nicks, nick)
	}
	nicksstr := strings.Join(nicks, " ")
	srvstr := ""
	captainsstr := ""
	if m.srv == nil {
		srvstr = Violet + "but there are no online servers in its pool =["
	} else {
		host := fmt.Sprintf("%s:%s", m.srv.host(), m.srv.port())
		if m.srv.password() != "" {
			srvstr = csprintf("{pink}{b}connect %s;password %s",
				host, m.srv.password())
		} else {
			srvstr = csprintf("{pink}{b}connect %s", host)
		}
	}
	if m.teamgame() && len(m.who) >= 2 {
		captainsstr = csprintf("{r} || team captains will be {red}%s{r} and {blue}%s{r}",
			m.cap1, m.cap2)
	}
	if m.srv != nil {
		s := csprintf("{cyan}%s{r} -> {dkblue}%s {green}[%d/%d] {orange}(%v)",
			m.srv.alias(), m.srv.hostname(), len(m.srv.clients()), m.srv.maxclients(), m.srv.ping())
		irc.privmsg(channel, s)
	}
	s := csprintf("{orange}{b}%s{b} is starting {r}-> %s {r}<- {orange}%s%s",
		m.name, srvstr, nicksstr, captainsstr)
	irc.privmsg(channel, s)
	// After a delay, PM everyone added.
	go func(nicks []string) {
		time.Sleep(2 * time.Second)
		for _, nick := range nicks {
			s := csprintf("{orange}{b}%s{b} is starting {r}-> %s {r}<- {orange}%s%s",
				m.name, srvstr, nick, captainsstr)
			irc.privmsg(nick, s)
			time.Sleep(60 * time.Millisecond)
		}
	}(nicks)
}

func readhist(fname string) ([]HistVal, error) {
	f, err := os.OpenFile(fname, os.O_RDONLY|os.O_CREATE, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewScanner(f)
	hist := make([]HistVal, 0)
	for r.Scan() {
		ss := strings.Split(r.Text(), "\t")
		if len(ss) != 3 {
			log.Println("bad entry in history")
			continue
		}
		t, err := time.Parse(tlayout, ss[0])
		if err != nil {
			log.Println(err)
			continue
		}
		hist = append(hist, HistVal{t, ss[1], ss[2]})
	}
	return hist, r.Err()
}

func appendhist(fname string, hist []HistVal) error {
	f, err := os.OpenFile(fname, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, h := range hist {
		t := h.t.Format(tlayout)
		if _, err = fmt.Fprintf(f, "%s\t%s\t%s\n", t, h.mode, h.nick); err != nil {
			return err
		}
	}
	return nil
}

func loggamestart(modename string, players []Player) {
	t := time.Now()
	newhist := make([]HistVal, 0, len(players))
	for i := range players {
		nick, _, _ := splituserstring(players[i].user)
		nick = strings.Trim(nick, "`^_")
		newhist = append(newhist, HistVal{t, modename, nick})
	}
	if err := appendhist(histfile, newhist); err != nil {
		log.Println(err)
	}
}

func execrc(fname string) error {
	f, err := os.OpenFile(fname, os.O_CREATE, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewScanner(f)
	for r.Scan() {
		cmd := strings.Split(r.Text(), " ")
		if len(cmd) < 1 {
			continue
		}
		cl := strings.ToLower(cmd[0])

		botfn, ok := botcmds[cl]
		if !ok {
			log.Printf("%s: no such command\n", cmd[0])
		}
		botfn.fn("", "", cmd[1:]...)
	}
	return nil
}

func appendrc(fname, cmd string, args []string) {
	f, err := os.OpenFile(fname, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0)
	if err != nil {
		log.Println(err)
		return
	}
	defer f.Close()
	a := strings.Join(args, " ")
	fmt.Fprintf(f, "%s %s\n", cmd, a)
}

func (cs Clients) String() string {
	ss := make([]string, len(cs))
	for i := range cs {
		ss[i] = cs[i].String()
	}
	return strings.Join(ss, ", ")
}

func (top Top10) Len() int      { return len(top) }
func (top Top10) Swap(i, j int) { top[i], top[j] = top[j], top[i] }
func (top Top10) Less(i, j int) bool {
	switch {
	case top[i].n > top[j].n:
		return true
	case top[i].n == top[j].n:
		return top[i].name < top[j].name
	}
	return false
}

func (top Top10) String() string {
	ss := make([]string, 0)
	for i := range top {
		ss = append(ss, fmt.Sprintf("%s (%d)", top[i].name, top[i].n))
	}
	return strings.Join(ss, ", ")
}

func top10month(where, who string, args ...string) bool {
	top := findtop(28 * 24 * time.Hour)
	if len(top) > 10 {
		top = top[:10]
	}
	topmsg := fmt.Sprintf("top 10 modes over the past month: %s", top)
	say(where, who, topmsg)
	return true
}

func top10week(where, who string, args ...string) bool {
	top := findtop(7 * 24 * time.Hour)
	if len(top) > 10 {
		top = top[:10]
	}
	topmsg := fmt.Sprintf("top 10 modes over the past week: %s", top)
	say(where, who, topmsg)
	return true
}

func top10players(where, who string, args ...string) bool {
	top := findtopplayers(7 * 24 * time.Hour)
	if len(top) > 10 {
		top = top[:10]
	}
	topmsg := fmt.Sprintf("top 10 players over the past week: %s", top)
	say(where, who, topmsg)
	return true
}

func top25players(where, who string, args ...string) bool {
	top := findtopplayers(28 * 24 * time.Hour)
	if len(top) > 25 {
		top = top[:25]
	}
	topmsg := fmt.Sprintf("top 10 players over the past week: %s", top)
	say(where, who, topmsg)
	return true
}

func topmost(where, who string, args ...string) bool {
	top := findtopplayers(time.Duration(1<<63 - 1))
	if len(top) > 10 {
		top = top[:10]
	}
	topmsg := fmt.Sprintf("top 10 players of all time: %s", top)
	say(where, who, topmsg)
	return true
}

func findtop(limit time.Duration) Top10 {
	t := time.Now()
	top := make(Top10, 0)
	count := make(map[string]int)
	hist, err := readhist(histfile)
	if err != nil {
		log.Println(err)
		return nil
	}
	for _, h := range hist {
		if t.Sub(h.t) > limit {
			break
		}
		count[h.mode] = count[h.mode] + 1
	}
	for k, v := range count {
		top = append(top, Top10Val{k, v})
	}
	sort.Sort(top)
	return top
}

func findtopplayers(limit time.Duration) Top10 {
	t := time.Now()
	top := make(Top10, 0)
	tally := make(map[string]int)
	hist, err := readhist(histfile)
	if err != nil {
		log.Println(err)
		return nil
	}
	for _, h := range hist {
		if t.Sub(h.t) > limit {
			break
		}
		tally[h.nick] = tally[h.nick] + 1
	}
	for k, v := range tally {
		top = append(top, Top10Val{k, v})
	}
	sort.Sort(top)
	return top
}

func showversion(where, who string, args ...string) bool {
	say(where, who, version)
	return true
}

func chkexpire() {
	removed := false
	now := time.Now()
	for i, m := range modes {
		for _, u := range m.who {
			if now.Before(u.expire) {
				continue
			}
			modes[i].removeplayer(u.user)
			removed = true
		}
	}
	if removed {
		updatetopic()
	}
}

func usage() {
	log.SetFlags(0)
	log.Fatal("usage: pkup [ flags ] host:port channel")
}

func init() {
	flag.Parse()
	if flag.NArg() != 2 {
		usage()
	}
	*ccflag = strings.Trim(*ccflag, "'\"")
	ccto = strings.Split(*ccflag, ",")
	host = flag.Arg(0)
	channel = flag.Arg(1)
	if err := execrc(runcommands); err != nil {
		log.SetFlags(0)
		log.Fatalln(err)
	}
	initial = false
}

func main() {
	irc = newIRCconn(*nick, *user, *real, "")
	if err := irc.dial(host); err != nil {
		log.Fatal(err)
	}
	var tick <-chan time.Time // Starts closed
	for {
		select {
		case ev := <-irc.Events:
			switch ev.cmd {
			case "welcome":
				irc.join(channel)
				updatetopic()
				tick = time.Tick(time.Minute)
			case "privmsg":
				if ev.msg[0] != '!' {
					break
				}
				cmd := strings.Split(ev.msg[1:], " ")
				cl := strings.ToLower(cmd[0])
				botfn, ok := botcmds[cl]
				if !ok {
					break
				}
				if botfn.op && !initial && !irc.isopped(ev.args[0], ev.src) {
					sayusage(ev.args[0], ev.src, ErrPermission)
					break
				}
				ok = botfn.fn(ev.args[0], ev.src, cmd[1:]...)
				if ok && botfn.save {
					appendrc(runcommands, cl, cmd[1:])
				}
			}
		case <-tick:
			chkexpire()
		}
	}
}
