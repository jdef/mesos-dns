// package records contains functions to generate resource records from
// mesos master states to serve through a dns server
package records

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/mesosphere/mesos-dns/logging"
)

// rrs is a type of question names to resource records answers
type rrs map[string][]string

type slave struct {
	Id       string `json:"id"`
	Hostname string `json:"hostname"`
}

// Slaves is a mapping of id to hostname read in from state.json
type Slaves []slave

// Resources holds our SRV ports
type Resources struct {
	Ports string `json:"ports"`
}

// Tasks holds mesos task information read in from state.json
type Tasks []struct {
	FrameworkId string `json:"framework_id"`
	Id          string `json:"id"`
	Name        string `json:"name"`
	SlaveId     string `json:"slave_id"`
	State       string `json:"state"`
	Resources   `json:"resources"`
}

// Frameworks holds mesos frameworks information read in from state.json
type Frameworks []struct {
	Tasks `json:"tasks"`
	Name  string `json:"name"`
}

// StateJSON is a representation of mesos master state.json
type StateJSON struct {
	Frameworks `json:"frameworks"`
	Slaves     `json:"slaves"`
	Leader     string `json:"leader"`
}

// RecordGenerator is a tmp mapping of resource records and slaves
// maybe de-dupe
// prob. want to break apart
// refactor me - prob. not needed
type RecordGenerator struct {
	As   rrs
	SRVs rrs
	Slaves
}

// hostBySlaveId looks up a hostname by slave_id
func (rg *RecordGenerator) hostBySlaveId(slaveId string) (string, error) {
	for i := 0; i < len(rg.Slaves); i++ {
		if rg.Slaves[i].Id == slaveId {
			return rg.Slaves[i].Hostname, nil
		}
	}

	return "", errors.New("not found")
}

// loadFromMaster loads state.json from mesos master
func (rg *RecordGenerator) loadFromMaster(ip string, port string) (sj StateJSON) {
	// tls ?
	url := "http://" + ip + ":" + port + "/master/state.json"

	req, err := http.NewRequest("GET", url, nil)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logging.Error.Println(err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logging.Error.Println(err)
	}

	err = json.Unmarshal(body, &sj)
	if err != nil {
		logging.Error.Println(err)
	}

	return sj
}

// leaderIP returns the ip for the mesos master
func leaderIP(leader string) string {
	pair := strings.Split(leader, "@")[1]
	return strings.Split(pair, ":")[0]
}

// loadWrap catches an attempt to load state.json from a mesos master
// attempts can fail from down server or mesos master secondary
// it also reloads from a different master if the master it attempted to
// load from was not the leader
func (rg *RecordGenerator) loadWrap(ip string, port string) (StateJSON, error) {
	var err error
	var sj StateJSON

	defer func() {
		if rec := recover(); rec != nil {
			err = errors.New("can't connect to mesos")
		}

	}()

	logging.VeryVerbose.Println("reloading from master " + ip)
	sj = rg.loadFromMaster(ip, port)

	if rip := leaderIP(sj.Leader); rip != ip {
		logging.VeryVerbose.Println("Warning: master changed to " + ip)
		sj = rg.loadFromMaster(rip, port)
	}

	return sj, err
}

// yankPorts takes an array of port ranges
func yankPorts(ports string) []string {
	rhs := strings.Split(ports, "[")[1]
	lhs := strings.Split(rhs, "]")[0]

	yports := []string{}

	mports := strings.Split(lhs, ",")
	for i := 0; i < len(mports); i++ {
		tmp := strings.TrimSpace(mports[i])
		pz := strings.Split(tmp, "-")
		lo, _ := strconv.Atoi(pz[0])
		hi, _ := strconv.Atoi(pz[1])

		for t := lo; t <= hi; t++ {
			yports = append(yports, strconv.Itoa(t))
		}
	}

	return yports
}

// findMaster tries each master and looks for the leader
// if no leader responds it errors
func (rg *RecordGenerator) findMaster(c *Config) (StateJSON, error) {
	var sj StateJSON

	if leader := c.getLeader(); leader != "" {
		logging.VeryVerbose.Println("Zookeeper says the leader is: ", leader)
		ip, port, err := getProto(leader)
		if err != nil {
			logging.Error.Println(err)
		}

		sj, _ = rg.loadWrap(ip, port)
		if sj.Leader == "" {
			logging.Verbose.Println("Warning: Zookeeper is wrong about leader")
			if len(c.Masters) == 0 {
				return sj, errors.New("no master")
			} else {
				logging.Verbose.Println("Warning: falling back to Masters config field: ", c.Masters)
			}
		} else {
			return sj, nil
		}
	}

	// try each listed mesos master before dying
	for i := 0; i < len(c.Masters); i++ {
		ip, port, err := getProto(c.Masters[i])
		if err != nil {
			logging.Error.Println(err)
		}

		sj, _ = rg.loadWrap(ip, port)

		if sj.Leader == "" {
			logging.VeryVerbose.Println("Warning: not a leader - trying next one")

			if len(c.Masters)-1 == i {
				return sj, errors.New("no master")
			}

		} else {
			return sj, nil
		}

	}

	return sj, nil
}

// should be able to accept
// ip:port
// zk://host1:port1,host2:port2,.../path
// zk://username:password@host1:port1,host2:port2,.../path
// file:///path/to/file (where file contains one of the above)
func getProto(pair string) (string, string, error) {
	h := strings.Split(pair, ":")
	return h[0], h[1], nil
}

// ParseState parses a state.json from a mesos master
// it sets the resource records map for the resolver
// with the following format
//
//  _<tag>.<service>.<framework>._<protocol>..mesos
// it also tries different mesos masters if one is not up
// this will shudown if it can't connect to a mesos master
func (rg *RecordGenerator) ParseState(config *Config) error {

	// try each listed mesos master before dying
	sj, err := rg.findMaster(config)
	if err != nil {
		logging.Error.Println("no master")
		return err
	}

	if sj.Leader == "" {
		logging.Error.Println("Unexpected error")
		err = errors.New("empty master")
		return err
	}

	rg.InsertState(sj, config.Domain, config.Mname, config.Listener, config.Masters)
	return nil
}

// cleanName sanitizes invalid characters
func cleanName(tname string) string {
	return stripInvalid(tname)
}

// stripInvalid remove any non-valid hostname characters
func stripInvalid(tname string) string {
	reg, err := regexp.Compile("[^\\w-.\\.]")
	if err != nil {
		logging.Error.Println(err)
	}

	s := reg.ReplaceAllString(tname, "")

	return strings.ToLower(strings.Replace(s, "_", "", -1))
}

const DNS952LabelFmt string = "[a-z]([-a-z0-9]*[a-z0-9])?"
const DNS952LabelMaxLength int = 24

var dns952LabelRegexp = regexp.MustCompile(DNS952LabelFmt)

// mangle the given name to be compliant as a DNS952 name "component".
// returns "" if the name cannot be mangled.
func asDNS952Label(name string) (label string) {
	name = strings.Replace(name, "_", "-", -1)
	name = strings.Replace(name, ".", "-", -1)
	for name != "" {
		if m := dns952LabelRegexp.FindStringIndex(name); m == nil {
			break
		} else {
			label += name[m[0]:m[1]]
			name = name[m[1]:]

		}
	}
	// don't allow the label to end with a hyphen, which could happen if
	// we truncate the previously valid label
	for x := len(label); x >= DNS952LabelMaxLength; {
		c := label[DNS952LabelMaxLength-1]
		if c == '-' {
			next := label[:DNS952LabelMaxLength-1]
			if x > DNS952LabelMaxLength {
				next += label[DNS952LabelMaxLength:]
			}
			label = next
			continue
		} else if x > DNS952LabelMaxLength {
			label = label[0:DNS952LabelMaxLength]
		}
		break
	}
	return
}

// InsertState transforms a StateJSON into RecordGenerator RRs
func (rg *RecordGenerator) InsertState(sj StateJSON, domain string, mname string,
	listener string, masters []string) error {
	rg.Slaves = sj.Slaves

	rg.SRVs = make(rrs)
	rg.As = make(rrs)

	f := sj.Frameworks

	// complete crap - refactor me
	for i := 0; i < len(f); i++ {
		fname := f[i].Name
		fname = cleanName(fname)

		for x := 0; x < len(f[i].Tasks); x++ {
			task := f[i].Tasks[x]

			host, err := rg.hostBySlaveId(task.SlaveId)
			if err == nil && (task.State == "TASK_RUNNING") {

				tname := asDNS952Label(task.Name)
				sid := slaveIdTail(task.SlaveId)
				tail := fname + "." + domain + "."

				// A records for task and task-sid
				arec := tname + "." + tail
				rg.insertRR(arec, host, "A")
				trec := tname + "-" + sid + "." + tail
				rg.insertRR(trec, host, "A")

				// SRV Records
				if task.Resources.Ports != "" {
					sports := yankPorts(task.Resources.Ports)

					// FIXME - 3 nested loops
					for s := 0; s < len(sports); s++ {
						//var srvhost string = tname + "." + fname + "." + domain + ":" + sports[s]
						var srvhost string = trec + ":" + sports[s]

						tcp := "_" + tname + "._tcp." + tail
						udp := "_" + tname + "._udp." + tail

						rg.insertRR(tcp, srvhost, "SRV")
						rg.insertRR(udp, srvhost, "SRV")
					}

				}

			}
		}
	}

	rg.listenerRecord(listener, mname)
	rg.masterRecord(domain, masters, sj.Leader)
	return nil
}

// listenerRecord sets the A record for the mesos-dns server in case
// there is a request for it's hostname (eg: from SOA mname)
func (rg *RecordGenerator) listenerRecord(listener string, mname string) {
	if listener == "0.0.0.0" {
		rg.setFromLocal(listener, mname)
	} else if listener == "127.0.0.1" {
		rg.insertRR(mname, "127.0.0.1", "A")
	} else {
		rg.insertRR(mname, listener, "A")
	}
}

// masterRecord sets A records for the mesos masters and an A record
// for the leading master
func (rg *RecordGenerator) masterRecord(domain string, masters []string, leader string) {
	// create records for leader
	// A records
	h := strings.Split(leader, "@")
	if len(h) < 2 {
		logging.Error.Println(leader)
	}
	ip, port, err := getProto(h[1])
	if err != nil {
		logging.Error.Println(err)
	}
	arec := "leader." + domain + "."
	rg.insertRR(arec, ip, "A")
	arec = "master." + domain + "."
	rg.insertRR(arec, ip, "A")
	// SRV records
	tcp := "_leader._tcp." + domain + "."
	udp := "_leader._udp." + domain + "."
	host := "leader." + domain + "." + ":" + port
	rg.insertRR(tcp, host, "SRV")
	rg.insertRR(udp, host, "SRV")

	for i := 0; i < len(masters); i++ {

		// skip leader
		if leader == masters[i] {
			continue
		}

		ip, _, err := getProto(masters[i])
		if err != nil {
			logging.Error.Println(err)
		}

		// A records (master and masterN)
		arec := "master." + domain + "."
		rg.insertRR(arec, ip, "A")
		arec = "master" + strconv.Itoa(i) + "." + domain + "."
		rg.insertRR(arec, ip, "A")
	}
}

// setFromLocal generates A records for each local interface we are
// listening on - if this causes problems you should explicitly set the
// listener address in config.json
func (rg *RecordGenerator) setFromLocal(host string, mname string) {

	ifaces, err := net.Interfaces()
	if err != nil {
		logging.Error.Println(err)
	}

	// handle err
	for _, i := range ifaces {

		addrs, err := i.Addrs()
		if err != nil {
			logging.Error.Println(err)
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() {
				continue
			}

			ip = ip.To4()
			if ip == nil {
				continue
			}

			rg.insertRR(mname, ip.String(), "A")
		}

	}
}

func stripHost(hostip string) string {
	return strings.Split(hostip, ":")[0]
}

func slaveIdTail(slaveID string) string {
	fields := strings.Split(slaveID, "-")
	return strings.ToLower(fields[len(fields)-1])
}

// insertRR inserts host to name's map
// refactor me
func (rg *RecordGenerator) insertRR(name string, host string, rtype string) {
	logging.VeryVerbose.Println("[" + rtype + "]\t" + name + ": " + host)

	if rtype == "A" {
		if val, ok := rg.As[name]; ok {

			h := stripHost(host)
			for _, b := range val {
				if stripHost(b) == h {
					return
				}
			}

			rg.As[name] = append(val, host)
		} else {
			rg.As[name] = []string{host}
		}
	} else {
		if val, ok := rg.SRVs[name]; ok {
			rg.SRVs[name] = append(val, host)
		} else {
			rg.SRVs[name] = []string{host}
		}
	}
}
