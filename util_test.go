package session

import "testing"

// go test -v -run=TestGetFreePort
func TestGetFreePort(t *testing.T) {
	port, err := GetFreePort(0)
	if err != nil {
		t.Error(err)
	}
	t.Log(port)
}
