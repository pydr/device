package device

import (
	"fmt"
	"testing"
)

func TestNewHost(t *testing.T) {
	device := NewDevice()
	config := &MonitorConfig{
		CPUTime:  10,
		MemTime:  10,
		NetTime:  1,
		DiskTime: 5,
		IOTime:   10,
	}
	go device.StartMonitor(config)
	for {
		select {
		case cpu := <-device.Cpu:
			fmt.Println(cpu.info())
		case mem := <-device.Memory:
			fmt.Println(mem.info())
		case net := <-device.Net:
			fmt.Println(net.info())
		case disks := <-device.Disks:
			for _, d := range disks {
				fmt.Println(d.info())
			}
			//case io := <-device.DiskIO:
			//	fmt.Println("io ->", io)
		}
	}
}
