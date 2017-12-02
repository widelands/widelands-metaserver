package main

type Command struct {

	data []byte
}

func NewCommand(c byte) *Command {
	cmd := &Command{
		data: make([]byte, 1),
	}
	cmd.data[0] = c
	return cmd
}

func (c *Command) AppendUInt(n uint8) {
	c.data = append(c.data, byte(n))
}

func (c *Command) AppendBytes(b []byte) {
	c.data = append(c.data, b...)
}


func (c *Command) AppendString(str string) {
	c.data = append(c.data, []byte(str)...)
	c.data = append(c.data, byte(0)) // '\0'
}

func (c *Command) GetBytes() ([]byte) {
	return c.data
}
