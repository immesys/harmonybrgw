package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
)

var HardcodedKeys map[string]string

var targeturl string

func init() {
	HardcodedKeys = make(map[string]string)
	//Add your keys here, e.g:
	HardcodedKeys["00ca"] = "f1597d0ee9178be2e8c650097db265b3"
	HardcodedKeys["01dc"] = "b940afdba21b8e5c21813b6fbb2f9442"
	HardcodedKeys["01dd"] = "c4557c075df8207e091abaa323048c2d"
	HardcodedKeys["01ea"] = "e5b5c412c35b7a53eb35846623f756d9"
	HardcodedKeys["01eb"] = "3b7986e691f49c39642f464032fa08a5"
	HardcodedKeys["0238"] = "7112224eac1e508d38d2ec75a5a1a9f3"
	//Set the environment variable
	targeturl = os.Getenv("TARGET_URL")
}

func HandleOnsiteDecode(em *egressmessage) {
	serial := binary.LittleEndian.Uint16(em.Payload[2:])
	sser := fmt.Sprintf("%04x", serial)
	key, ok := HardcodedKeys[sser]
	if !ok {
		fmt.Printf("[%04x] dropping hamilton-3c-v2 packet: no key found\n", serial)
		return
	}
	bkey, err := hex.DecodeString(key)
	if err != nil {
		panic(err)
	}
	block, err := aes.NewCipher(bkey)
	if err != nil {
		panic(err)
	}
	iv := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F}
	dce := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(em.Payload)-4)
	dce.CryptBlocks(plaintext, em.Payload[4:])
	for i := 40; i < 48; i++ {
		if plaintext[i] != 0 {
			fmt.Printf("[%04x] dropping hamilton-3c-v2 packet because it looks like AES key is wrong\n", serial)
			return
		}
	}

	f_uptime := binary.LittleEndian.Uint64(plaintext[0:8])
	f_flags := binary.LittleEndian.Uint16(plaintext[8:10])
	f_acc_x := int16(binary.LittleEndian.Uint16(plaintext[10:12]))
	f_acc_y := int16(binary.LittleEndian.Uint16(plaintext[12:14]))
	f_acc_z := int16(binary.LittleEndian.Uint16(plaintext[14:16]))
	f_mag_x := int16(binary.LittleEndian.Uint16(plaintext[16:18]))
	f_mag_y := int16(binary.LittleEndian.Uint16(plaintext[18:20]))
	f_mag_z := int16(binary.LittleEndian.Uint16(plaintext[20:22]))
	f_tmp_die := int16(binary.LittleEndian.Uint16(plaintext[22:24]))
	f_tmp_val := binary.LittleEndian.Uint16(plaintext[24:26])
	f_hdc_tmp := int16(binary.LittleEndian.Uint16(plaintext[26:28]))
	f_hdc_rh := binary.LittleEndian.Uint16(plaintext[28:30])
	f_light_lux := binary.LittleEndian.Uint16(plaintext[30:32])
	f_buttons := binary.LittleEndian.Uint16(plaintext[32:34])
	f_occup := binary.LittleEndian.Uint16(plaintext[34:36])
	dat := make(map[string]interface{})
	dat["serial"] = fmt.Sprintf("%04x", serial)
	dat["uptime"] = float64(f_uptime)
	if f_flags&(1<<0) != 0 {
		//accel
		dat["acc_x"] = float64(f_acc_x) * 0.244
		dat["acc_y"] = float64(f_acc_y) * 0.244
		dat["acc_z"] = float64(f_acc_z) * 0.244

	}
	if f_flags&(1<<1) != 0 {
		dat["mag_x"] = float64(f_mag_x) * 0.1
		dat["mag_y"] = float64(f_mag_y) * 0.1
		dat["mag_z"] = float64(f_mag_z) * 0.1
	}
	if f_flags&(1<<2) != 0 {
		//TMP
		dat["tp_die_temp"] = float64(int16(f_tmp_die)>>2) * 0.03125
		uv := float64(int16(f_tmp_val)) * 0.15625
		dat["tp_voltage"] = uv
	}

	if f_flags&(1<<3) != 0 {
		//HDC
		rh := float64(f_hdc_rh) / 100
		t := float64(f_hdc_tmp) / 100
		dat["air_temp"] = t
		dat["air_rh"] = rh
		expn := (17.67 * t) / (t + 243.5)
		dat["air_hum"] = (6.112 * math.Pow(math.E, expn) * rh * 2.1674) / (273.15 + t)
	}
	if f_flags&(1<<4) != 0 {
		//LUX
		dat["lux"] = math.Pow(10, float64(f_light_lux)/(65536.0/5.0))
	}
	if f_flags&(1<<5) != 0 {
		dat["button_events"] = float64(f_buttons)
	}
	if f_flags&(1<<6) != 0 {
		dat["presence"] = float64(f_occup) / 32768
	}

	dat["time"] = float64(em.Brtime)
	jsonValue, _ := json.Marshal(dat)
	if targeturl == "" {
		fmt.Printf("not posting, TARGET_URL not set, body was: \n%v\n", string(jsonValue))
		return
	}
	resp, err := http.Post(targeturl, "application/json", bytes.NewBuffer(jsonValue))
	if err != nil {
		fmt.Printf("failed to publish: %v\n", err)
	} else {
		fmt.Printf("push ok to %s\n", targeturl)
	}
	if resp != nil {
		resp.Body.Close()
	}
}
