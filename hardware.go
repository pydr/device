package device

import (
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/deniswernert/go-fstab"
	"github.com/toolkits/nux"
)

var (
	procStatHistory [2]*nux.ProcStat
	ifsStatHistory  [2]map[string]int64
	ioStatHistory   [2]map[string]*nux.DiskStats
	psLock          = new(sync.RWMutex)
	cpuUsageRate    float64
	netInBandwidth  float64
	netOutBandwidth float64
	netInPackets    int64
	netOutPackets   int64

	diskIoData = make(map[string]*IO)
)

const (
	B  = 1
	KB = 1024 * B
	MB = 1024 * KB
	GB = 1024 * MB

	ByteBit = 8
	BIT     = 1
	KBIT    = 1000 * BIT
	MBIT    = 1000 * KBIT

	CpuSampleTime = 1
	NetSampleTime = 1
	IoSampleTime  = 2

	SectorBytes = 512
)

type (
	Cpu struct {
		UsageRate float64 `json:"usage_rate"`
		Loadavg   float64 `json:"loadavg"`
	}

	Memory struct {
		Used      uint64  `json:"used"`
		Total     uint64  `json:"total"`
		Free      uint64  `json:"free"`
		UsageRate float64 `json:"usage_rate"`
	}

	Disk struct {
		MountPoint string  `json:"mount_point"`
		Total      uint64  `json:"total"`
		Used       uint64  `json:"used"`
		Free       uint64  `json:"free"`
		UsageRate  float64 `json:"usage_rate"`
	}

	Net struct {
		InBandwidth  float64 `json:"in_bandwidth"`
		OutBandwidth float64 `json:"out_bandwidth"`
		InPackets    int64   `json:"in_packets"`
		OutPackets   int64   `json:"out_packets"`
		TCPConns     int64   `json:"tcp_conns"`
	}

	// 磁盘IO状态
	// Source: https://linuxtools-rst.readthedocs.io/zh_CN/latest/tool/iostat.html
	IO struct {
		IORPs    float64 // 每秒读次数
		IOWPs    float64 // 每秒写次数
		RBytesPs float64 // 读字节速率
		WBytesPs float64 // 写字节速率
		Await    float64 // 平均每次设备I/O操作的等待时间 (毫秒)。
		IOUtil   float64 // 每秒用于io操作的时间占比。ioTime / 1000 ( ioTime 的单位为毫秒)
	}
)

func init() {
	go updateCpuUsageRate() // 采集cpu率用率
	go updateNetIo()        // 采集网络信息
	go updateDiskIO()       // 采集磁盘io状态
}

type Hardware interface {
	info() string
}

func (c *Cpu) info() string {
	return fmt.Sprintf("[CPU] >>> useage rate: %.2f, loadavg: %.2f.",
		c.UsageRate, c.Loadavg)
}

func (m *Memory) info() string {
	return fmt.Sprintf("[Memory] >>> used: %.2f MB, free: %.2f MB, total: %.2f MB, useage rate: %.2f",
		float64(m.Used/GB), float64(m.Free/MB), float64(m.Total/MB), m.UsageRate)
}

func (d *Disk) info() string {
	return fmt.Sprintf("[Disk] >>> mount point: %s, used: %.2f GB, free: %.2f GB, total: %.2f GB, useage rate: %.2f",
		d.MountPoint, float64(d.Used/GB), float64(d.Free/GB), float64(d.Total/GB), d.UsageRate)
}

func (n *Net) info() string {
	return fmt.Sprintf("[Network] >>> in bandwidth: %.2f Mbps, out bandwidth: %.2f Mbps, in packets: %d, out packets: %d, tcp connections: %d",
		n.InBandwidth, n.OutBandwidth, n.InPackets, n.OutPackets, n.TCPConns)
}

// get cpu loadavg
func getLoadavg() (float64, error) {
	loadavg, err := nux.LoadAvg()
	if err != nil {
		return 0, err
	}

	return loadavg.Avg1min, nil
}

func updateCpuStat() error {
	ps, err := nux.CurrentProcStat()
	if err != nil {
		return err
	}

	psLock.Lock()
	defer psLock.Unlock()
	for i := 1; i > 0; i-- {
		procStatHistory[i] = procStatHistory[i-1]
	}
	procStatHistory[0] = ps

	return nil
}

func updateCpuUsageRate() {
	for {
		time.Sleep(CpuSampleTime * time.Second)
		err := updateCpuStat()
		if err != nil {
			continue
		}
		if procStatHistory[1] == nil {
			continue
		}

		psLock.RLock()
		dt := procStatHistory[0].Cpu.Total - procStatHistory[1].Cpu.Total
		invQuotient := 100.00 / float64(dt)

		cpuUsageRate = 100 - float64(procStatHistory[0].Cpu.Idle-procStatHistory[1].Cpu.Idle)*invQuotient
		psLock.RUnlock()
	}
}

// get cpu status
func getCpuStatus() (*Cpu, error) {
	cpu := Cpu{}
	loadavg, err := getLoadavg()
	if err != nil {
		return nil, err
	}

	cpu.UsageRate = cpuUsageRate
	cpu.Loadavg = loadavg
	return &cpu, nil
}

func diskUsage(path string) (*Disk, error) {
	disk := Disk{}
	fs := syscall.Statfs_t{}
	err := syscall.Statfs(path, &fs)
	if err != nil {
		return nil, err
	}

	disk.MountPoint = path
	disk.Total = fs.Blocks * uint64(fs.Bsize)
	disk.Free = fs.Bfree * uint64(fs.Bsize)
	disk.Used = disk.Total - disk.Free
	return &disk, nil
}

// get storage status
func getDiskUsage() (map[string]*Disk, error) {
	disks := make(map[string]*Disk)
	mounts, _ := fstab.ParseSystem()
	for _, val := range mounts {
		//fmt.Printf("%v\n", val.String())
		if val.File == "swap" || val.File == "/dev/shm" || val.File == "/dev/pts" ||
			val.File == "/proc" || val.File == "/sys" || val.File == "none" {
			continue
		}
		disk, err := diskUsage(val.File)
		if err != nil {
			return nil, err
		}

		disk.UsageRate = float64(disk.Used) / float64(disk.Total) * 100
		disks[val.File] = disk
	}

	return disks, nil
}

// get memory status
func getMemStatus() (*Memory, error) {
	mem := Memory{}
	memStatus, err := nux.MemInfo()
	if err != nil {
		return nil, err
	}

	mem.Used = memStatus.MemTotal - memStatus.MemAvailable
	mem.Total = memStatus.MemTotal
	mem.Free = memStatus.MemAvailable
	mem.UsageRate = float64(mem.Used) / float64(memStatus.MemTotal) * 100

	return &mem, nil
}

func getIfsInfo() (map[string]int64, error) {
	ifsInfo := make(map[string]int64)
	ifs := []string{""}
	nets, err := nux.NetIfs(ifs)
	if err != nil {
		return nil, err
	}

	for _, net := range nets {
		if net.Iface == "lo" {
			continue
		}
		ifsInfo["inBytesTotal"] += net.InBytes
		ifsInfo["outBytesTotal"] += net.OutBytes
		ifsInfo["inPacketsTotal"] += net.InPackages
		ifsInfo["outPacketsTotal"] += net.OutPackages
	}

	return ifsInfo, nil
}

func updateIfsStat() error {
	ipfStat, err := getIfsInfo()
	if err != nil {
		return err
	}

	psLock.Lock()
	defer psLock.Unlock()
	for i := 1; i > 0; i-- {
		ifsStatHistory[i] = ifsStatHistory[i-1]
	}
	ifsStatHistory[0] = ipfStat

	return nil
}

func updateNetIo() {
	for {
		time.Sleep(NetSampleTime * time.Second)
		err := updateIfsStat()
		if err != nil {
			continue
		}
		if ifsStatHistory[1] == nil {
			continue
		}

		// net in bandwidth
		psLock.RLock()
		inDelta := ifsStatHistory[0]["inBytesTotal"] - ifsStatHistory[1]["inBytesTotal"]
		netInBandwidth = float64(inDelta*ByteBit) / float64(MBIT) / NetSampleTime // realtime net speed kb/s (download)

		// net out bandwidth
		outDelta := ifsStatHistory[0]["outBytesTotal"] - ifsStatHistory[1]["outBytesTotal"]
		netOutBandwidth = float64(outDelta*ByteBit) / float64(MBIT) / NetSampleTime // realtime net speed kb/s (upload)

		// net in packets
		inPacketDelta := ifsStatHistory[0]["inPacketsTotal"] - ifsStatHistory[1]["inPacketsTotal"]
		netInPackets = inPacketDelta / NetSampleTime

		// net out packets
		outPacketDelta := ifsStatHistory[0]["outPacketsTotal"] - ifsStatHistory[1]["outPacketsTotal"]
		netOutPackets = outPacketDelta / NetSampleTime
		psLock.RUnlock()
	}
}

// get network status
func getNetStatus() (*Net, error) {
	net := Net{}
	net.InBandwidth = netInBandwidth                // net in bandwidth
	net.OutBandwidth = netOutBandwidth              // net out bandwidth
	net.InPackets = netInPackets                    // net in packets
	net.OutPackets = netOutPackets                  // net out packets
	if tcpMap, err := nux.Snmp("Tcp"); err == nil { // get realtime tcp connected
		if currEstab, ok := tcpMap["CurrEstab"]; ok {
			net.TCPConns = currEstab
		}
	}

	return &net, nil
}

func isDisk(deviceName string) bool {
	path := "/sys/block/" + deviceName + "/device"
	_, err := os.Stat(path)
	if err != nil {
		return os.IsExist(err)
	}

	return true
}

func getIoInfo() (map[string]*nux.DiskStats, error) {
	ioInfo := make(map[string]*nux.DiskStats)
	disks, err := nux.ListDiskStats()
	if err != nil {
		return nil, err
	}

	for _, disk := range disks {
		if !isDisk(disk.Device) {
			continue
		}
		ioInfo[disk.Device] = disk
	}

	return ioInfo, nil
}

func updateIoStat() error {
	ioInfo, err := getIoInfo()
	if err != nil {
		return err
	}

	psLock.Lock()
	defer psLock.Unlock()
	for i := 1; i > 0; i-- {
		ioStatHistory[i] = ioStatHistory[i-1]
	}
	ioStatHistory[0] = ioInfo

	return nil
}

func updateDiskIO() {
	for {
		time.Sleep(IoSampleTime * time.Second)
		err := updateIoStat()
		if err != nil {
			continue
		}
		if ioStatHistory[1] == nil {
			continue
		}
		for diskName1, diskData1 := range ioStatHistory[0] {
			io := IO{}
			for diskName2, diskData2 := range ioStatHistory[1] {
				if diskName1 == diskName2 {
					rDelta := diskData1.ReadRequests - diskData2.ReadRequests
					io.IORPs = float64(rDelta) / IoSampleTime

					wDelta := diskData1.WriteRequests - diskData2.WriteRequests
					io.IOWPs = float64(wDelta) / IoSampleTime

					rDataDelta := diskData1.ReadSectors - diskData2.ReadSectors
					io.RBytesPs = (float64(rDataDelta) / IoSampleTime) * SectorBytes / float64(KB)

					wDataDelta := diskData1.WriteSectors - diskData2.WriteSectors
					io.WBytesPs = (float64(wDataDelta) / IoSampleTime) * SectorBytes / float64(KB)

					msecDelta := (diskData1.MsecRead - diskData2.MsecRead) + (diskData1.MsecWrite - diskData2.MsecWrite)
					if rDelta+wDelta != 0 {
						io.Await = float64(msecDelta) / float64(rDelta+wDelta)
					} else {
						io.Await = 0
					}

					io.IOUtil = float64(msecDelta) / IoSampleTime / 1000

					psLock.Lock()
					diskIoData[diskName1] = &io
					psLock.Unlock()
				}
			}
		}
	}
}

// get disk io status
func getDiskIO() map[string]*IO {
	psLock.RLock()
	defer psLock.RUnlock()
	return diskIoData
}
