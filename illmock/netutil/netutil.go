package netutil

import "net"

func GetFreeListener() (*net.TCPListener, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return nil, err
	}

	return net.ListenTCP("tcp", addr)
}

// getFreePort asks the kernel for a free open port that is ready to use.
func GetFreePort() (int, error) {
	l, err := GetFreeListener()
	if err != nil {
		return 0, err
	}
	// release for now so it can be bound by the actual server
	// a more robust solution would be to bind the server to the port and close it here
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
