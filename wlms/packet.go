package main

import (
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
)

type Packet struct {
	data []string
}

func (p *Packet) ReadInt() (int, error) {
	d, err := p.ReadString()
	if err != nil {
		return 0, err
	}
	i, err := strconv.Atoi(d)
	log.Printf("d: %v, i: %v\n", d, i)
	if err != nil {
		return 0, fmt.Errorf("Invalid integer: '%s'", d)
	}
	return i, nil
}

func (
	p *Packet) ReadBool() (bool, error) {
	d, err := p.ReadString()
	if err != nil {
		return false, err
	}
	switch d {
	case "0", "false":
		return false, nil
	case "1", "true":
		return true, nil
	default:
		return false, fmt.Errorf("Invalid bool: '%s'", d)
	}
}

func (p *Packet) ReadString() (string, error) {
	if len(p.data) == 0 {
		return "", fmt.Errorf("No more data in the packet.")
	}
	d := p.data[0]
	p.data = p.data[1:]
	return d, nil
}

func readInt(r io.Reader) (int, error) {
	buf := make([]byte, 2)

	// NOCOM(sirver): this is BS. If this is this high, the tests never pass.
	// But lower makes no sense for production.
	// NOCOM(sirver): no longer with deadlines :(
	// r.SetDeadline(time.Now().Add(60 * time.Second))
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, err
	}
	return (int(buf[0]) << 8) + int(buf[1]), nil
}

func readString(r io.Reader, nlen int) (string, error) {
	buf := make([]byte, nlen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func ReadPacket(r io.Reader) (*Packet, error) {
	nlen, err := readInt(r)
	if err != nil {
		return nil, err
	}
	str, err := readString(r, nlen-2)
	if err != nil {
		return nil, err
	}
	returnValue := strings.Split(str, "\x00")
	return &Packet{returnValue[:len(returnValue)-1]}, nil
}

func BuildPacket(data ...interface{}) []byte {
	nbytes := 2
	for _, v := range data {
		switch v.(type) {
		case int:
			nbytes += len(strconv.Itoa(v.(int))) + 1
		case string:
			nbytes += len(v.(string)) + 1
		case bool:
			if v.(bool) {
				nbytes += len("true") + 1
			} else {
				nbytes += len("false") + 1
			}
		default:
			log.Fatal("Unknown type in BuildPacket.")
		}
	}

	buf := make([]byte, nbytes)
	buf[0] = byte((nbytes & 0xff00) >> 8)
	buf[1] = byte(nbytes & 0xff)
	i := 2
	for _, v := range data {
		as_string := ""
		switch v.(type) {
		case int:
			as_string = strconv.Itoa(v.(int))
		case string:
			as_string = v.(string)
		case bool:
			if v.(bool) {
				as_string = "true"
			} else {
				as_string = "false"
			}
		}

		copy(buf[i:i+len(as_string)], as_string)
		i += len(as_string)
		buf[i] = 0x00
		i += 1
	}
	return buf
}
