package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync/atomic"
	"time"

	"periph.io/x/periph/host"
	"periph.io/x/periph/host/rpi"
)

var OurPopID string
var BaseURI string
var EntityBlob []byte
var totaltx int

const HeartbeatTimeout = 2 * time.Second
const BlinkInterval = 200 * time.Millisecond
const HbTypeMcuToPi = 1
const HbTypePiToMcu = 2

var PILED = rpi.P1_22

var MCUBuildNumber uint32

const BRGWBuildNumber = 602

var LedChan chan int

const FULLOFF = 1
const FULLON = 2
const BLINKING1 = 3
const BLINKING2 = 4
const BLINKING3 = 5
const BadAge = 2 * time.Hour
const MaxBadAgeTrigger = 1 * time.Hour

var WanChan chan int

var puberror uint64
var pubsucc uint64

var BRName string

func writeMessage(conn net.Conn, message []byte) error {
	hdr := make([]byte, 4)
	binary.BigEndian.PutUint32(hdr[:], uint32(len(message)))
	_, err := conn.Write(hdr) //binary.Write(conn, binary.BigEndian, len(message))
	if err != nil {
		return err
	}
	_, err = conn.Write(message)
	return err
}

func readMessage(conn net.Conn) ([]byte, error) {
	hdr := make([]byte, 4)
	_, err := io.ReadFull(conn, hdr)
	if err != nil {
		return nil, err
	}
	msgsize := binary.BigEndian.Uint32(hdr[0:])
	buf := make([]byte, msgsize, msgsize)
	_, err = io.ReadFull(conn, buf)
	return buf, err
}

func die() {
	os.Exit(1)
}
func processIncomingHeartbeats() {
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: "@rethos/4", Net: "unix"})
	if err != nil {
		fmt.Printf("heartbeat socket: error: %v\n", err)
		die()
	}
	gotHeartbeat := make(chan bool, 1)
	hbokay := make(chan bool, 1)
	go func() {
		for {
			select {
			case <-time.After(HeartbeatTimeout):
				hbokay <- false
				continue
			case <-gotHeartbeat:
				hbokay <- true
				continue
			}
		}
	}()
	go func() {
		for wanstate := range WanChan {
			_ = wanstate
			msg := make([]byte, 4)
			msg[0] = HbTypePiToMcu
			msg[1] = byte(wanstate)
			msg[2] = 0x55
			msg[3] = 0xAA
			err := writeMessage(conn, msg)
			if err != nil {
				fmt.Printf("got wanstate error: %v\n", err)
				os.Exit(10)
			}
		}
	}()
	go func() {
		okaycnt := 0
		for {
			select {
			case x := <-hbokay:
				if x {
					okaycnt++
					if okaycnt > 5 {
						LedChan <- FULLON
						okaycnt = 5
					}
				} else {
					LedChan <- BLINKING1
					okaycnt = 0
				}
			}
		}
	}()
	fmt.Println("hearbeat socket: connected ok")
	for {
		buf, err := readMessage(conn)
		if err != nil {
			fmt.Printf("heartbeat socket: error: %v\n", err)
			die()
		}
		num := len(buf)
		if num >= 16 && binary.LittleEndian.Uint32(buf) == HbTypeMcuToPi {
			gotHeartbeat <- true
			McuVer := binary.LittleEndian.Uint32(buf[12:])
			atomic.StoreUint32(&MCUBuildNumber, McuVer)
		} else {
			hbokay <- false
		}
	}
}

const ResetInterval = 30 * time.Second

var hasInternet bool

func checkInternet() {
	for {
		//You would typically put some code here that verifies that the internet is ok
		//and sets hasInternet
		hasInternet = true

		time.Sleep(10 * time.Second)
	}
}

func processWANStatus() {
	for {
		if hasInternet {
			WanChan <- FULLON
		} else {
			WanChan <- FULLOFF
		}
		time.Sleep(500 * time.Millisecond)
	}
}

type LinkStats struct {
	BadFrames           uint64 `msgpack:"bad_frames"`
	LostFrames          uint64 `msgpack:"lost_frames"`
	DropNotConnected    uint64 `msgpack:"drop_not_connected"`
	SumSerialReceived   uint64 `msgpack:"sum_serial_received"`
	SumDomainForwarded  uint64 `msgpack:"sum_domain_forwarded"`
	SumDropNotConnected uint64 `msgpack:"drop_not_connected"`
	SumDomainReceived   uint64 `msgpack:"sum_domain_received"`
	SumSerialForwarded  uint64 `msgpack:"sum_serial_forwarded"`
	BRGW_PubOK          uint64 `msgpack:"br_pub_ok"`
	BRGW_PubERR         uint64 `msgpack:"br_pub_err"`
	MCUBuild            uint32 `msgpack:"mcu_version"`
	BRGWBuild           uint32 `msgpack:"brgw_version"`
}

func processStats() {
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: "@rethos/0", Net: "unix"})
	if err != nil {
		fmt.Printf("heartbeat socket: error: %v\n", err)
		die()
	}
	for {
		buf, err := readMessage(conn)
		if err != nil {
			fmt.Printf("Unix socket error: %v\n", err)
			os.Exit(1)
		}
		num := len(buf)
		if num < 10256 {
			fmt.Printf("Abort malformed stats frame, length %d\n", num)
			os.Exit(1)
		}
		ls := LinkStats{}
		ls.MCUBuild = atomic.LoadUint32(&MCUBuildNumber)
		ls.BRGWBuild = BRGWBuildNumber
		idx := 4 //Skip the first four fields
		ls.BadFrames = binary.LittleEndian.Uint64(buf[idx*8:])
		idx++
		ls.LostFrames = binary.LittleEndian.Uint64(buf[idx*8:])
		idx++
		ls.DropNotConnected = binary.LittleEndian.Uint64(buf[idx*8:])
		idx++
		for i := 0; i < 255; i++ {
			serial_received := binary.LittleEndian.Uint64(buf[idx*8:])
			idx++
			domain_forwarded := binary.LittleEndian.Uint64(buf[idx*8:])
			idx++
			drop_notconnected := binary.LittleEndian.Uint64(buf[idx*8:])
			idx++
			domain_received := binary.LittleEndian.Uint64(buf[idx*8:])
			idx++
			serial_forwarded := binary.LittleEndian.Uint64(buf[idx*8:])
			idx++
			ls.SumSerialReceived += serial_received
			ls.SumDomainForwarded += domain_forwarded
			ls.SumDropNotConnected += drop_notconnected
			ls.SumDomainReceived += domain_received
			ls.SumSerialForwarded += serial_forwarded
		}
		ls.BRGW_PubERR = puberror
		ls.BRGW_PubOK = pubsucc
		//Do something with these stats
	}
}

func processIncomingData() {
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: "@rethos/5", Net: "unix"})
	if err != nil {
		fmt.Printf("heartbeat socket: error: %v\n", err)
		die()
	}
	for {
		buf, err := readMessage(conn)
		if err != nil {
			fmt.Printf("data socket: error: %v\n", err)
			die()
		}
		num := len(buf)
		frame, ok := unpack(buf[:num])
		if !ok {
			fmt.Println("bad frame")
			continue
		}
		HandleOnsiteDecode(frame)
	}
}

func LedAnim(ledchan chan int) {
	state := FULLOFF
	lastval := false
	go func() {
		for x := range ledchan {
			state = x
			if state == FULLOFF {
				PILED.Out(false)
			}
			if state == FULLON {
				PILED.Out(true)
			}
		}
	}()
	for {
		<-time.After(BlinkInterval)
		if state == FULLOFF {
			PILED.Out(false)
		}
		if state == FULLON {
			PILED.Out(true)
		}
		if state == BLINKING1 {
			if lastval {
				PILED.Out(false)
			} else {
				PILED.Out(true)
			}
			lastval = !lastval
		}
	}
}
func main() {
	if _, err := host.Init(); err != nil {
		log.Fatal(err)
	}
	LedChan = make(chan int, 1)
	WanChan = make(chan int, 1)
	go LedAnim(LedChan)
	go checkInternet()
	go processIncomingHeartbeats()
	go processWANStatus()
	go processStats()
	processIncomingData()
}

func unpack(frame []byte) (*egressmessage, bool) {
	if len(frame) < 38 {
		return nil, false
	}
	fs := egressmessage{
		//skip 0:4 - len+ cksum
		Srcmac:  fmt.Sprintf("%012x", frame[2:10]),
		Srcip:   net.IP(frame[10:26]).String(),
		Popid:   OurPopID,
		Poptime: int64(binary.LittleEndian.Uint64(frame[26:34])),
		Brtime:  time.Now().UnixNano(),
		Rssi:    int(frame[34]),
		Lqi:     int(frame[35]),
		Payload: frame[36:],
	}
	return &fs, true
}

type egressmessage struct {
	Srcmac  string `msgpack:"srcmac"`
	Srcip   string `msgpack:"srcip"`
	Popid   string `msgpack:"popid"`
	Poptime int64  `msgpack:"poptime"`
	Brtime  int64  `msgpack:"brtime"`
	Rssi    int    `msgpack:"rssi"`
	Lqi     int    `msgpack:"lqi"`
	Payload []byte `msgpack:"payload"`
}
