package test_utils

type FakeAddr struct{}

func (a FakeAddr) Network() string {
	return "TestingNetwork"
}
func (a FakeAddr) String() string {
	return "TestingString"
}
