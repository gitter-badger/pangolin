package main

import (
	"github.com/ant0ine/go-json-rest/rest"
	. "github.com/mattn/go-getopt"
	"github.com/satori/go.uuid"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var lock = sync.RWMutex{}
var zpool string
var listen string
var piddir string
var conlogdir string
var quit chan string
var conportbase int

type Instances struct {
	Instance string
	Running  bool
	Image    string
	ConPort  int
	Cpu      int
	Mem      int
}

type Images struct {
	Imageid string
	Os      string
}

type Ima struct {
	Ima string
	Mem int
	Cpu int
}

func init() {
	var c int
	var pubinterface string
	// defaults
	listen = ":8080"
	piddir = "/var/run"
	conlogdir = "/tmp"
	conportbase = 10000
	quit = make(chan string)

	OptErr = 0
	for {
		if c = Getopt("z:l:p:i:c:b:h"); c == EOF {
			break
		}
		switch c {
		case 'z':
			zpool = OptArg
		case 'c':
			conlogdir = OptArg
		case 'l':
			listen = OptArg
		case 'p':
			piddir = OptArg
		case 'i':
			pubinterface = OptArg
		case 'b':
			cpb, _ := strconv.Atoi(OptArg)
			conportbase = cpb
		case 'h':
			usage()
			os.Exit(1)
		}
	}

	if zpool == "" {
		println("zpool required")
		usage()
		os.Exit(1)
	}

	if pubinterface == "" {
		println("public interface required")
		usage()
		os.Exit(1)
	}

	loadKmod("vmm")
	loadKmod("nmdm")
	loadKmod("if_bridge")
	loadKmod("if_tap")

	sysctlSet("net.link.tap.up_on_open", "1")
	bridgeCreate()
	bridgeAddPub(pubinterface)
	restartCons()

}

// restart consoles on running instances
func restartCons() {
	for _, inst := range getInstances() {
		if inst.Running {
			go startRecordedWebConsole(inst.Instance)
		}
	}
}

func usage() {
	println("usage: pangolin [-z zpool|-l listenaddress|-p piddir|-i publicinterface|-c consolelogdir|-h]")
}

func bridgeCreate() {
	lock.Lock()
	cmd := exec.Command("sudo", "ifconfig", "bridge0", "create")
	cmd.Output()
	cmd = exec.Command("sudo", "ifconfig", "bridge0", "up")
	cmd.Output()
	lock.Unlock()
}

func bridgeAddPub(publicinterface string) {
	lock.Lock()
	cmd := exec.Command("sudo", "ifconfig", "bridge0", "addm", publicinterface)
	cmd.Output()
	lock.Unlock()
}

func sysctlSet(sysctl string, value string) {
	lock.Lock()
	cmd := exec.Command("sudo", "/sbin/sysctl", sysctl+"="+value)
	cmd.Output()
	lock.Unlock()
}

func loadKmod(module string) {
	lock.Lock()
	cmd := exec.Command("kldstat", "-m", module)
	_, err := cmd.Output()
	lock.Unlock()

	if err == nil {
		return
	}

	lock.Lock()
	cmd = exec.Command("sudo", "kldload", module)
	_, err = cmd.Output()
	lock.Unlock()
}

func main() {
	api := rest.NewApi()
	api.Use(rest.DefaultDevStack...)
	router, err := rest.MakeRouter(
		rest.Post("/api/v1/images", HandleImageCreate),
		rest.Get("/api/v1/images", HandleImageList),
		rest.Post("/api/v1/instances", HandleInstanceCreate),
		rest.Post("/api/v1/instances/:instanceid", HandleInstanceStart),
		rest.Put("/api/v1/instances/:instanceid", HandleInstanceStop),
		rest.Get("/api/v1/instances", HandleInstanceList),
		rest.Get("/api/v1/instances/:instanceid", HandleInstanceInfo),
		rest.Delete("/api/v1/instances/:instanceid", HandleInstanceDestroy),
	)
	if err != nil {
		log.Fatal(err)
	}
	api.SetApp(router)
	log.Fatal(http.ListenAndServe(listen, api.MakeHandler()))
}

func HandleImageList(w rest.ResponseWriter, r *rest.Request) {
	lock.Lock()
	cmd := exec.Command("zfs", "list", "-H", "-t", "volume")
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		return
	}

	lines := strings.Split(string(stdout), "\n")
	imas := make([]Images, 0)

	for _, line := range lines {
		if strings.Contains(line, "ima-") {
			ima := Images{}
			n := strings.Split(line, "\t")[0]
			n = strings.Split(n, "/")[1]
			ima.Imageid = n
			ima.Os = getImaOs(ima.Imageid)
			imas = append(imas, ima)
		}
	}

	w.WriteJson(imas)
}

func getInstanceIma(instanceid string) string {
	cmd := exec.Command("zfs", "get", "-H", "origin", zpool+"/"+instanceid)
	stdout, err := cmd.Output()

	if err != nil {
		return ""
	}
	if len(strings.Fields(string(stdout))) < 2 {
		return ""
	}
	origin := strings.Fields(string(stdout))[2]
	origin = strings.Split(origin, "/")[1]
	origin = strings.Split(origin, "@")[0]

	return origin
}

func getImaOs(imageid string) string {
	lock.Lock()
	cmd := exec.Command("sudo", "zfs", "get", "-H", "pangolin:os", zpool+"/"+imageid)
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		return ""
	}
	if len(strings.Fields(string(stdout))) < 2 {
		return ""
	}
	os := strings.Fields(string(stdout))[2]

	return os

}

func getInstances() []Instances {
	lock.Lock()
	cmd := exec.Command("zfs", "list", "-H", "-t", "volume")
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		println(stdout)
		return nil
	}

	lines := strings.Split(string(stdout), "\n")

	instance_list := make([]Instances, 0)
	re, err := regexp.Compile(`^i-.*`)

	for _, line := range lines {
		if len(line) > 0 {
			instanceid := strings.Split(line, "\t")[0]
			instanceid = strings.Split(instanceid, "/")[1]
			if re.MatchString(instanceid) == true {
				inst := getInstance(instanceid)
				instance_list = append(instance_list, inst)
			}
		}
	}

	return instance_list
}

func getInstance(instanceid string) Instances {
	inst := Instances{}
	inst.Instance = instanceid
	_, err := getPid(inst.Instance)
	if err == nil {
		inst.Running = true
	}
	inst.Image = getInstanceIma(instanceid)
	inst.ConPort = getConPort(instanceid)
	inst.Cpu = getCpu(instanceid)
	inst.Mem = getMem(instanceid)

	return inst
}

func HandleInstanceList(w rest.ResponseWriter, r *rest.Request) {
	w.WriteJson(getInstances())
}

func HandleInstanceInfo(w rest.ResponseWriter, r *rest.Request) {
	instanceid := r.PathParam("instanceid")
	w.WriteJson(getInstance(instanceid))
}

func cloneIma(ima string, instanceid string) {
	cmd := exec.Command("sudo", "zfs", "clone", "-o", "volmode=dev", zpool+"/"+ima+"@0", zpool+"/"+instanceid)
	stdout, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	print(string(stdout))
}

func destroyClone(instanceid string) {
	cmd := exec.Command("sudo", "zfs", "destroy", zpool+"/"+instanceid)
	_, err := cmd.Output()

	if err != nil {
		return
	}
}

func addTapToBridge(tap string, bridge string) {
	lock.Lock()
	cmd := exec.Command("sudo", "ifconfig", bridge, "addm", tap)
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		println("addTapToBridge error: ")
		println(err.Error())
		return
	}

	print(string(stdout))
}

func bridgeUp(bridge string) {
	lock.Lock()
	cmd := exec.Command("sudo", "ifconfig", bridge, "up")
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		println(err.Error())
		return
	}

	print(string(stdout))
}

func bhyveLoad(console string, memory int, instanceid string) {
	cmd := exec.Command("sudo", "bhyveload", "-c", console, "-m", strconv.Itoa(memory)+"M", "-d", "/dev/zvol/"+zpool+"/"+instanceid, instanceid)
	stdout, err := cmd.CombinedOutput()

	if err != nil {
		println(err.Error())
		return
	}

	print(string(stdout))
}

func bhyveDestroy(instanceid string) {
	cmd := exec.Command("sudo", "bhyvectl", "--destroy", "--vm", instanceid)
	_, err := cmd.Output()

	if err != nil {
		return
	}
}

func execBhyve(console string, cpus int, memory int, tap string, instanceid string) {
	pidfile := piddir + "/pangolin." + instanceid + ".pid"
	cmd := exec.Command("sudo", "daemon", "-c", "-f", "-p", pidfile, "bhyve", "-c", strconv.Itoa(cpus), "-m", strconv.Itoa(memory), "-H", "-A", "-P", "-s", "0:0,hostbridge", "-s", "1:0,lpc", "-s", "2:0,virtio-net,"+tap, "-s", "3:0,virtio-blk,/dev/zvol/"+zpool+"/"+instanceid, "-lcom1,"+console, instanceid)
	stdout, err := cmd.CombinedOutput()

	if err != nil {
		println(err.Error())
		return
	}
	print(string(stdout))
}

func allocateTap() string {
	lock.Lock()
	cmd := exec.Command("sudo", "ifconfig", "tap", "create")
	stdout, err := cmd.Output()
	lock.Unlock()
	if err != nil {
		println(err.Error())
		return ""
	}

	tap := string(stdout)
	if tap[len(tap)-1:] == "\n" {
		tap = tap[:len(tap)-1]
	}
	return tap
}

func freeTap(tap string) {
	// TODO check that name begins with "tap"
	cmd := exec.Command("sudo", "ifconfig", tap, "destroy")
	cmd.Output()
}

func findBridge() string {
	// TODO create separate bridge for each instance to separate instances
	return "bridge0"
}

func saveInstanceProperty(instanceid string, property string, value string) {
	cmd := exec.Command("sudo", "zfs", "set", "pangolin:"+property+"="+value, zpool+"/"+instanceid)
	stdout, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	print(string(stdout))

}

func getInstanceProperty(instanceid string, property string) string {
	cmd := exec.Command("zfs", "get", "-H", "-s", "local", "pangolin:"+property, zpool+"/"+instanceid)
	stdout, err := cmd.Output()
	if err != nil {
		return ""
	}
	if len(strings.Fields(string(stdout))) < 2 {
		return ""
	}
	value := strings.Fields(string(stdout))[2]
	return value
}

func saveTap(tap string, instanceid string) {
	saveInstanceProperty(instanceid, "tap", tap)
}

func saveCpu(cpu int, instanceid string) {
	saveInstanceProperty(instanceid, "cpu", strconv.Itoa(cpu))
}

func saveMem(mem int, instanceid string) {
	saveInstanceProperty(instanceid, "mem", strconv.Itoa(mem))
}

func getTap(instanceid string) string {
	return getInstanceProperty(instanceid, "tap")
}

func getCpu(instanceid string) int {
	cpu, _ := strconv.Atoi(getInstanceProperty(instanceid, "cpu"))
	return cpu
}

func getMem(instanceid string) int {
	mem, _ := strconv.Atoi(getInstanceProperty(instanceid, "mem"))
	return mem
}

func getPid(instanceid string) (string, error) {
	pidfile := piddir + "/pangolin." + instanceid + ".pid"
	lock.Lock()
	cmd := exec.Command("sudo", "cat", pidfile)
	stdout, err := cmd.Output()
	lock.Unlock()
	if err != nil {
		return "", err
	}
	return string(stdout), nil
}

func getConPort(instanceid string) int {
	tap := getTap(instanceid)
	if tap == "" {
		return -1
	}
	if tap == "-1" {
		return -1
	}
	tapi, _ := strconv.Atoi(tap[3:])
	return tapi + conportbase
}

// takes an image id and creates a running instance from it
func HandleInstanceCreate(w rest.ResponseWriter, r *rest.Request) {
	// get ima
	ima := Ima{}
	err := r.DecodeJsonPayload(&ima)
	if err != nil {
		rest.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ima.Ima == "" {
		rest.Error(w, "ima required", 400)
		return
	}
	if ima.Mem == 0 {
		rest.Error(w, "memory required", 400)
		return
	}
	if ima.Cpu == 0 {
		rest.Error(w, "cpu required", 400)
		return
	}

	// start the instance
	os := getImaOs(ima.Ima)
	switch os {
	case "freebsd":
		// clone ima to instance
		instanceid := allocateInstanceId()
		cloneIma(ima.Ima, instanceid)

		// create network interface and bring up
		tap := allocateTap()
		if tap == "" {
			return
		}
		saveTap(tap, instanceid)
		bridge := findBridge()
		addTapToBridge(tap, bridge)
		bridgeUp(bridge)

		// cleanup leftover instance if needed
		bhyveDestroy(instanceid)
		nmdm := "/dev/nmdm-" + instanceid + "-A"
		saveCpu(ima.Cpu, instanceid)
		saveMem(ima.Mem, instanceid)
		go startFreeBSDVM(nmdm, ima.Cpu, ima.Mem, tap, instanceid)
		w.WriteJson(&instanceid)
	case "linux":
		// clone ima to instance
		instanceid := allocateInstanceId()
		cloneIma(ima.Ima, instanceid)

		// create network interface and bring up
		tap := allocateTap()
		if tap == "" {
			return
		}
		saveTap(tap, instanceid)
		bridge := findBridge()
		addTapToBridge(tap, bridge)
		bridgeUp(bridge)

		//nmdm := "/dev/nmdm-" + instanceid + "-A"
		saveCpu(ima.Cpu, instanceid)
		saveMem(ima.Mem, instanceid)
		// bhyveLoad(nmdm, ima.Mem, instanceid)
		// execBhyve(nmdm, ima.Cpu, ima.Mem, tap, instanceid)
		w.WriteJson(&instanceid)
	default:
		rest.Error(w, "unknown OS", 400)
	}
}

func startFreeBSDVM(console string, cpus int, memory int, tap string, instanceid string) {
	// cleanup leftover instance if needed
	bhyveDestroy(instanceid)
	bhyveLoad(console, memory, instanceid)
	execBhyve(console, cpus, memory, tap, instanceid)
	go startRecordedWebConsole(instanceid)
}

func killGotty(instanceid string) {
	cmd := exec.Command("sudo", "ps", "auxww")
	stdout, err := cmd.Output()
	if err != nil {
		return
	}

	lines := strings.Split(string(stdout), "\n")
	var recpid string

	for _, line := range lines {
		if strings.Contains(line, instanceid) {
			sudo := strings.Contains(line, "sudo")
			if !sudo {
				bhyve := strings.Contains(line, "bhyve")
				if !bhyve {
					recpid = strings.Fields(line)[1]
					cmd = exec.Command("sudo", "kill", recpid)
					cmd.Output()
				}
			}
		}
	}
	for _, line := range lines {
		if strings.Contains(line, instanceid) {
			gotty := strings.Contains(line, "gotty")
			if gotty {
				recpid = strings.Fields(line)[1]
				cmd = exec.Command("sudo", "kill", recpid)
				cmd.Output()
			}
		}
	}
}

func startGotty(instanceid string, port int) {
	cmd := exec.Command("gotty", "--title-format", instanceid, "--once", "-w", "-p", strconv.Itoa(port), "ttyrec", "-a", "-e", "sudo cu -l /dev/nmdm-"+instanceid+"-B", conlogdir+"/"+instanceid+".rec")
	cmd.Start()
	cmd.Wait()
}

func startRecordedWebConsole(instanceid string) {
	for {
		select {
		case msg := <-quit:
			if msg == instanceid {
				return
			} else {
				quit <- msg
			}
		default:
			killGotty(instanceid)
			port := getConPort(instanceid)
			if port != -1 {
				startGotty(instanceid, port)
			}
		}
	}
}

func killRecordedWebConsole(instanceid string) {
	go func() {
		quit <- instanceid
	}()
	killGotty(instanceid)
}

func allocateInstanceId() string {
	u1 := uuid.NewV4()
	u2 := u1.String()
	u2 = "i-" + u2[0:8]
	return u2
}

func killInstance(instanceid string) {
	killRecordedWebConsole(instanceid)
	pid, _ := getPid(instanceid)
	if len(pid) > 0 {
		cmd := exec.Command("sudo", "kill", pid)
		cmd.Output()
	}

	var pidstate error
	pidstate = nil
	for pidstate == nil {
		cmd := exec.Command("sudo", "kill", "-0", pid)
		err := cmd.Start()
		if err != nil {
			log.Fatal(err)
		}
		pidstate = cmd.Wait()
		time.Sleep(500 * time.Millisecond)
	}

	bhyveDestroy(instanceid)
}

func HandleInstanceStart(w rest.ResponseWriter, r *rest.Request) {
	instanceid := r.PathParam("instanceid")

	re, _ := regexp.Compile(`^i-.*`)
	if re.MatchString(instanceid) == false {
		return
	}

	_, err := getPid(instanceid)
	if err == nil {
		w.WriteJson(&instanceid)
		return
	}

	ima := getInstanceIma(instanceid)
	os := getImaOs(ima)

	switch os {
	case "freebsd":
		// create network interface and bring up
		tap := allocateTap()
		if tap == "" {
			return
		}
		saveTap(tap, instanceid)
		bridge := findBridge()
		addTapToBridge(tap, bridge)
		bridgeUp(bridge)

		// start the instance
		nmdm := "/dev/nmdm-" + instanceid + "-A"
		cpu := getCpu(instanceid)
		mem := getMem(instanceid)
		go startFreeBSDVM(nmdm, cpu, mem, tap, instanceid)
		w.WriteJson(&instanceid)
	default:
		rest.Error(w, "unknown OS", 400)
	}

}

func HandleInstanceStop(w rest.ResponseWriter, r *rest.Request) {
	instanceid := r.PathParam("instanceid")

	re, _ := regexp.Compile(`^i-.*`)
	if re.MatchString(instanceid) == false {
		return
	}

	go realInstanceStop(instanceid)

	return
}

func realInstanceStop(instanceid string) {
	killInstance(instanceid)
	tap := getTap(instanceid)
	if len(tap) > 0 {
		freeTap(tap)
	}
	saveTap("-1", instanceid)
}

func realInstanceDestroy(instance string) {
	realInstanceStop(instance)

	// wait for VM to stop
	time.Sleep(500 * time.Millisecond)

	destroyClone(instance)
}

func HandleInstanceDestroy(w rest.ResponseWriter, r *rest.Request) {
	instance := r.PathParam("instanceid")

	re, _ := regexp.Compile(`^i-.*`)
	if re.MatchString(instance) == false {
		return
	}

	go realInstanceDestroy(instance)

	w.WriteJson(&instance)
}

func HandleImageCreate(w rest.ResponseWriter, r *rest.Request) {
	u1 := uuid.NewV4()
	u2 := u1.String()
	u2 = "ima-" + u2[0:8]

	lock.Lock()
	// TODO make this not hard coded, allow uploading data for instance, etc
	cmd := exec.Command("echo", "zfs", "clone", zpool+"/bhyve01@2015081817020001", zpool+"/"+u2)
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		println(err.Error())
		return
	}

	print(string(stdout))

	w.WriteJson(&u2)
}
