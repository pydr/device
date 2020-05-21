package device

import (
	"time"

	. "github.com/iocn-io/zlog"
	"go.uber.org/zap"
)

type Host struct {
	Cpu    chan *Cpu
	Memory chan *Memory
	Net    chan *Net
	Disks  chan map[string]*Disk
	DiskIO chan map[string]*IO
	Time   time.Time
}

func (h *Host) updateCpuStatus(t time.Duration) {
	for {
		cpuStatus, err := getCpuStatus()
		if err != nil || cpuStatus == nil {
			Zlog.Warn("get CPU status failed: ", zap.Error(err))
			time.Sleep(t * time.Second)
			continue
		}
		h.Cpu <- cpuStatus
		time.Sleep(t * time.Second)
	}
}

func (h *Host) updateMemStatus(t time.Duration) {
	for {
		memStatus, err := getMemStatus()
		if err != nil || memStatus == nil {
			Zlog.Warn("get Memory status failed: ", zap.Error(err))
			time.Sleep(t * time.Second)
			continue
		}
		h.Memory <- memStatus
		time.Sleep(t * time.Second)
	}
}

func (h *Host) updateNetStatus(t time.Duration) {
	for {
		netStatus, err := getNetStatus()
		if err != nil || netStatus == nil {
			Zlog.Warn("get Net status failed: ", zap.Error(err))
			time.Sleep(t * time.Second)
			continue
		}
		h.Net <- netStatus
		time.Sleep(t * time.Second)
	}
}

func (h *Host) updateDiskUsage(t time.Duration) {
	for {
		diskStatus, err := getDiskUsage()
		if err != nil || diskStatus == nil {
			Zlog.Warn("get Disk status failed: ", zap.Error(err))
			time.Sleep(t * time.Second)
			continue
		}
		h.Disks <- diskStatus
		time.Sleep(t * time.Second)
	}
}

func (h *Host) updateDiskIoStatus(t time.Duration) {
	for {
		diskIO := getDiskIO()
		if diskIO == nil {
			Zlog.Warn("get Disk IO status failed")
			time.Sleep(t * time.Second)
			continue
		}
		h.DiskIO <- diskIO
		time.Sleep(t * time.Second)
	}
}

func NewDevice() *Host {
	return &Host{
		Cpu:    make(chan *Cpu, 2),
		Memory: make(chan *Memory, 2),
		Net:    make(chan *Net, 2),
		Disks:  make(chan map[string]*Disk, 2),
		DiskIO: make(chan map[string]*IO, 2),
	}
}

type (
	// start monitor config (unit: second)
	MonitorConfig struct {
		CPUTime  time.Duration // CPU monitoring interval
		MemTime  time.Duration // memory monitoring interval
		NetTime  time.Duration // network monitoring interval
		DiskTime time.Duration // disk monitoring interval
		IOTime   time.Duration // disk io monitoring interval
	}
)

func (h *Host) StartMonitor(config *MonitorConfig) {
	go h.updateCpuStatus(config.CPUTime)
	go h.updateMemStatus(config.MemTime)
	go h.updateNetStatus(config.NetTime)
	go h.updateDiskUsage(config.DiskTime)
	//go h.updateDiskIoStatus(config.IOTime)
}
