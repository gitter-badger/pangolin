package main

import (
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/satori/go.uuid"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	//"github.com/mistifyio/go-zfs"
	//"fmt"
)

// TODO read config file
var zpool = "boxy"

func main() {
	api := rest.NewApi()
	api.Use(rest.DefaultDevStack...)
	router, err := rest.MakeRouter(

		// TODO http://docs.aws.amazon.com/AWSEC2/latest/APIReference/OperationList-query.html

		rest.Post("/api/v1/images", ImageCreate),
		rest.Get("/api/v1/images", ImageList),

		rest.Post("/api/v1/instances", InstanceCreate),
		rest.Post("/api/v1/instances/:instanceid", InstanceStart),
		rest.Put("/api/v1/instances/:instanceid", InstanceStop),
		rest.Get("/api/v1/instances", InstanceList),
		rest.Delete("/api/v1/instances/:instanceid", InstanceDestroy),
	)
	if err != nil {
		log.Fatal(err)
	}
	api.SetApp(router)
	log.Fatal(http.ListenAndServe(":8080", api.MakeHandler()))
}

type Instances struct {
	Instance string
}

type Ima struct {
	Ima string
}

var lock = sync.RWMutex{}

func ImageList(w rest.ResponseWriter, r *rest.Request) {

	lock.Lock()
	cmd := exec.Command("zfs", "list", "-H", "-t", "volume")
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		println(err.Error())
		return
	}

	lines := strings.Split(string(stdout), "\n")
	imas := make([]string, 0)

	for _, line := range lines {
		if strings.Contains(line, "ima-") {
			n := strings.Split(line, "\t")[0]
			n = strings.Split(n, "/")[1]
			imas = append(imas, n)
		}
	}

	w.WriteJson(imas)
}

func InstanceList(w rest.ResponseWriter, r *rest.Request) {
	lock.Lock()
	cmd := exec.Command("zfs", "list", "-H", "-t", "volume")
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		println(stdout)
		return
	}

	lines := strings.Split(string(stdout), "\n")

	instance_list := make([]Instances, 0)
	re, err := regexp.Compile(`^i-.*`)

        // TODO return instance status (running or stopped)
	for _, line := range lines {
		if len(line) > 0 {
			i := strings.Split(line, "\t")[0]
			i = strings.Split(i, "/")[1]
			if re.MatchString(i) == true {
				inst := Instances{}
				inst.Instance = i
				instance_list = append(instance_list, inst)
			}
		}
	}
	w.WriteJson(&instance_list)
}

func cloneIma(ima string, instanceid string) {
	cmd := exec.Command("sudo", "zfs", "clone", zpool+"/"+ima+"@0", zpool+"/"+instanceid)
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

func setupTap(tap string) {
	lock.Lock()
	cmd := exec.Command("sudo", "ifconfig", tap, "create")
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		println(err.Error())
		return
	}

	print(string(stdout))
}

func addTapToBridge(tap string, bridge string) {
	lock.Lock()
	cmd := exec.Command("sudo", "ifconfig", bridge, "addm", tap)
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
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
	lock.Lock()
	cmd := exec.Command("sudo", "bhyveload", "-c", console, "-m", strconv.Itoa(memory)+"M", "-d", "/dev/zvol/"+zpool+"/"+instanceid, instanceid)
	stdout, err := cmd.Output()
	lock.Unlock()

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
	pidfile := "/var/tmp/pangolin." + instanceid + ".pid"
	lock.Lock()
	cmd := exec.Command("sudo", "daemon", "-c", "-f", "-p", pidfile, "bhyve", "-c", strconv.Itoa(cpus), "-m", strconv.Itoa(memory), "-H", "-A", "-P", "-s", "0:0,hostbridge", "-s", "1:0,lpc", "-s", "2:0,virtio-net,"+tap, "-s", "3:0,virtio-blk,/dev/zvol/"+zpool+"/"+instanceid, "-lcom1,"+console, instanceid)
	stdout, err := cmd.Output()
	lock.Unlock()

	if err != nil {
		println(err.Error())
		return
	}
	print(string(stdout))
}

func allocateTap() string {
	// TODO bring tap up? net.link.tap.up_on_open=1 should ensure it, but
	// paranoia, perhaps should have setupTap check state of
	// net.link.tap.up_on_open or do it at startup after reading config
	lock.Lock()
	cmd := exec.Command("ifconfig")
	stdout, err := cmd.Output()
	lock.Unlock()
	if err != nil {
		println(err.Error())
		return ""
	}

	lines := strings.Split(string(stdout), "\n")

	t := 0
	r, err := regexp.Compile("^tap" + strconv.Itoa(t) + ": .*")

	for _, line := range lines {
		if r.MatchString(line) == true {
			t = t + 1
			r, err = regexp.Compile("^tap" + strconv.Itoa(t) + ": .*")
		}
	}

	return "tap" + strconv.Itoa(t)
}

func allocateNmdm() string {
	cmd := exec.Command("ls", "/dev/")
	stdout, err := cmd.Output()
	if err != nil {
		return ""
	}

	nmdm := 0
	lines := strings.Split(string(stdout), "\n")
	r, err := regexp.Compile("^nmdm" + strconv.Itoa(nmdm) + "+A")

	for _, line := range lines {
		if r.MatchString(line) == true {
			nmdm = nmdm + 1
			r, err = regexp.Compile("^nmdm" + strconv.Itoa(nmdm) + "+A")
		}
	}

	return "nmdm" + strconv.Itoa(nmdm)

}

func freeTap(tap string) {
	// TODO check that name beings with "tap"
	cmd := exec.Command("sudo", "ifconfig", tap, "destroy")
	cmd.Output()
}

func findBridge() string {
	// TODO create separate bridge for each instance to separate instances
	return "bridge0"
}

func saveTap(tap string, instanceid string) {
	cmd := exec.Command("sudo", "zfs", "set", "pangolin:tap="+tap, zpool+"/"+instanceid)
	stdout, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	print(string(stdout))
}

func saveNmdm(nmdm string, instanceid string) {
	cmd := exec.Command("sudo", "zfs", "set", "pangolin:nmdm="+nmdm, zpool+"/"+instanceid)
	stdout, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	print(string(stdout))
}

func getTap(instanceid string) string {
	cmd := exec.Command("zfs", "get", "-H", "-s", "local", "pangolin:tap", zpool+"/"+instanceid)
	stdout, err := cmd.Output()
	if err != nil {
		return ""
	}
	if len(strings.Fields(string(stdout))) < 2 {
		return ""
	}
	tap := strings.Fields(string(stdout))[2]
	return tap
}

func getNmdm(instanceid string) string {
	cmd := exec.Command("zfs", "get", "-H", "-s", "local", "pangolin:nmdm", zpool+"/"+instanceid)
	stdout, err := cmd.Output()
	if err != nil {
		return ""
	}
	if len(strings.Fields(string(stdout))) < 2 {
		return ""
	}
	tap := strings.Fields(string(stdout))[2]
	return tap
}

func getPid(instanceid string) string {
	pidfile := "/var/tmp/pangolin." + instanceid + ".pid"
	lock.Lock()
	cmd := exec.Command("sudo", "cat", pidfile)
	stdout, err := cmd.Output()
	lock.Unlock()
	if err != nil {
		return ""
	}
	return string(stdout)
}

// takes an image id and creates a running instance from it
func InstanceCreate(w rest.ResponseWriter, r *rest.Request) {
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

	// clone ima to instance
	u1 := uuid.NewV4()
	u2 := u1.String()
	u2 = "i-" + u2[0:8]

	cloneIma(ima.Ima, u2)

	// create network interface and bring up
	tap := allocateTap()
	if tap == "" {
		return
	}
	bridge := findBridge()
	setupTap(tap)
	saveTap(tap, u2)
	addTapToBridge(tap, bridge)
	bridgeUp(bridge)

	// start the instance
	bhyveDestroy(u2)
	nmdm := allocateNmdm()
	if nmdm == "" {
		return
	}
	saveNmdm(nmdm, u2)
	bhyveLoad("/dev/"+nmdm+"A", 512, u2)
	execBhyve("/dev/"+nmdm+"A", 1, 512, tap, u2)
	w.WriteJson(&u2)
}

func killInstance(instance string) {
	pid := getPid(instance)
	if len(pid) > 0 {
		exec.Command("sudo", "kill", pid)
	}
	bhyveDestroy(instance)
}

func InstanceStart(w rest.ResponseWriter, r *rest.Request) {
	instance := r.PathParam("instanceid")

	re, _ := regexp.Compile(`^i-.*`)
	if re.MatchString(instance) == false {
		return
	}

	// create network interface and bring up
	tap := getTap(instance)
	if tap == "" {
		return
	}
	bridge := findBridge()
	setupTap(tap)
	addTapToBridge(tap, bridge)
	bridgeUp(bridge)

	// start the instance
	bhyveDestroy(instance)
	nmdm := getNmdm(instance)
	if nmdm == "" {
		return
	}
	bhyveLoad("/dev/"+nmdm+"A", 512, instance)
	execBhyve("/dev/"+nmdm+"A", 1, 512, tap, instance)
	w.WriteJson(&instance)

}

func InstanceStop(w rest.ResponseWriter, r *rest.Request) {
	instance := r.PathParam("instanceid")

	re, _ := regexp.Compile(`^i-.*`)
	if re.MatchString(instance) == false {
		return
	}

	killInstance(instance)
        return
}

func InstanceDestroy(w rest.ResponseWriter, r *rest.Request) {
	instance := r.PathParam("instanceid")

	re, _ := regexp.Compile(`^i-.*`)
	if re.MatchString(instance) == false {
		return
	}

	killInstance(instance)

	tap := getTap(instance)
	if len(tap) > 0 {
		freeTap(tap)
	}

        // wait for VM to stop
	time.Sleep(1000 * time.Millisecond)

	destroyClone(instance)

	w.WriteJson(&instance)
}

// TODO make this not hard coded, allow uploading data for instance, etc
func ImageCreate(w rest.ResponseWriter, r *rest.Request) {
	u1 := uuid.NewV4()
	u2 := u1.String()
	u2 = "ima-" + u2[0:8]

	lock.Lock()
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