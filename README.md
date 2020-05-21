## Device
a linux device monitor program

update: 05/28/2019



### Quick Start

---

#### installation

```bash
go get github.com/pydr/device
```

#### import the package

```go
import "github.com/pydr/device"
```

#### get node status data

```go
device := NewDevice()
config := &MonitorConfig{
    CPUTime:  10,
    MemTime:  10,
    NetTime:  1,
    DiskTime: 5 * 60,
    IOTime:   10,
}
go device.StartMonitor(config)
for {
    select {
    case cpu := <-device.Cpu:
        fmt.Println("cpu -> ", cpu)
    case mem := <-device.Memory:
        fmt.Println("mem -> ", mem)
    case net := <-device.Net:
        fmt.Println("net ->", net)
    case disk := <-device.Disk:
        fmt.Println("disk ->", disk)
    case io := <-device.DiskIO:
        fmt.Println("io ->", io)
    }
}
```

