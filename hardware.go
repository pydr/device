package device

import (
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
		Used      float64 `json:"used"`
		Total     float64 `json:"total"`
		UsageRate float64 `json:"usage_rate"`
	}

	Device struct {
		Total     uint64
		Used      uint64
		Free      uint64
		UsageRate float64
	}

	Disk struct {
		Total     float64 `json:"total"`
		Used      float64 `json:"used"`
		Free      float64 `json:"free"`
		UsageRate float64 `json:"usage_rate"`
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

func diskUsage(path string) (*Device, error) {
	device := Device{}
	fs := syscall.Statfs_t{}
	err := syscall.Statfs(path, &fs)
	if err != nil {
		return nil, err
	}

	device.Total = fs.Blocks * uint64(fs.Bsize)
	device.Free = fs.Bfree * uint64(fs.Bsize)
	device.Used = device.Total - device.Free
	return &device, nil
}

// get storage status
func getDiskUsage() (*Disk, error) {
	var (
		diskTotal float64
		diskUsed  float64
		diskFree  float64
		usageRate float64
	)

	disk := Disk{}
	mounts, _ := fstab.ParseSystem()
	for _, val := range mounts {
		//fmt.Printf("%v\n", val.File)
		if val.File == "swap" || val.File == "/dev/shm" || val.File == "/dev/pts" ||
			val.File == "/proc" || val.File == "/sys" || val.File == "none" {
			continue
		}
		disk, err := diskUsage(val.File)
		if err != nil {
			return nil, err
		}

		diskTotal += float64(disk.Total) / float64(GB)
		diskUsed += float64(disk.Used) / float64(GB)
		diskFree += float64(disk.Free) / float64(GB)
	}

	usageRate = diskUsed / diskTotal * 100
	disk.Total = diskTotal
	disk.Used = diskUsed
	disk.Free = diskFree
	disk.UsageRate = usageRate

	return &disk, nil
}

// get memory status
func getMemStatus() (*Memory, error) {
	mem := Memory{}
	memStatus, err := nux.MemInfo()
	if err != nil {
		return nil, err
	}

	mem.Used = float64(memStatus.MemTotal - memStatus.MemAvailable)
	mem.Total = float64(memStatus.MemTotal)
	mem.UsageRate = mem.Used / float64(memStatus.MemTotal) * 100

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
