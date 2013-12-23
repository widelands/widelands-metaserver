package main

import (
	"io"
	"log"
	"strconv"
	"strings"
)

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

func ReadPacket(r io.Reader) ([]string, error) {
	nlen, err := readInt(r)
	if err != nil {
		return []string{}, err
	}
	str, err := readString(r, nlen-2)
	if err != nil {
		return []string{}, err
	}
	returnValue := strings.Split(str, "\x00")
	return returnValue[:len(returnValue)-1], nil
}

func (client *Client) SendPacket(data ...interface{}) {
	client.conn.Write(BuildPacket(data...))
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
