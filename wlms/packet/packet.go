package packet

import (
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
)

type Packet struct {
	RawData []string
}

func New(rawData ...interface{}) []byte {
	nbytes := 2
	for _, v := range rawData {
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
			log.Fatalf("Unknown type in packet.New(), got %T", v)
		}
	}

	buf := make([]byte, nbytes)

	// Length of package.
	buf[0] = byte((nbytes & 0xff00) >> 8)
	buf[1] = byte(nbytes & 0xff)

	i := 2
	for _, v := range rawData {
		var as_string string
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

func Read(r io.Reader) (*Packet, error) {
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

func (p *Packet) Unpack(returnValues ...interface{}) error {
	for _, ptr := range returnValues {
		var err error
		switch ptr := ptr.(type) {
		case *int:
			*ptr, err = p.ReadInt()
		case *bool:
			*ptr, err = p.ReadBool()
		case *string:
			*ptr, err = p.ReadString()
		default:
			log.Fatal("Unknown type in Unpack().")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Packet) ReadInt() (int, error) {
	d, err := p.ReadString()
	if err != nil {
		return 0, err
	}
	i, err := strconv.Atoi(d)
	if err != nil {
		return 0, fmt.Errorf("Invalid integer: '%s'", d)
	}
	return i, nil
}

func (p *Packet) ReadBool() (bool, error) {
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
	if len(p.RawData) == 0 {
		return "", fmt.Errorf("No more RawData in the packet.")
	}
	d := p.RawData[0]
	p.RawData = p.RawData[1:]
	return d, nil
}

func readInt(r io.Reader) (int, error) {
	buf := make([]byte, 2)
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
