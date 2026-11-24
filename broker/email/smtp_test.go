package email

import (
	"bufio"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultMailerUsesFinalDataResponse(t *testing.T) {
	for _, tc := range []struct {
		name         string
		dataResponse string
		quitResponse string
		wantError    bool
	}{
		{name: "server closes after accepting DATA", dataResponse: "250 Queued"},
		{name: "server rejects QUIT after accepting DATA", dataResponse: "250 Queued", quitResponse: "500 QUIT rejected"},
		{name: "server rejects DATA", dataResponse: "550 Message rejected", wantError: true},
		{name: "server closes before acknowledging DATA", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
			require.NoError(t, err)
			t.Cleanup(func() { _ = listener.Close() })
			require.NoError(t, listener.SetDeadline(time.Now().Add(5*time.Second)))
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				done <- serveSMTPDelivery(conn, tc.dataResponse, tc.quitResponse)
			}()

			err = (&DefaultMailer{}).SendMail(listener.Addr().String(), nil, "from@example.com", []string{"to@example.com"},
				[]byte("Subject: Test\r\n\r\nA pull-slip message\r\n"))
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, <-done)
		})
	}
}

func serveSMTPDelivery(conn net.Conn, dataResponse, quitResponse string) error {
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	reader := textproto.NewReader(bufio.NewReader(conn))
	send := func(reply string) error {
		_, err := fmt.Fprintf(conn, "%s\r\n", reply)
		return err
	}
	exchange := func(prefix, reply string) error {
		cmd, err := reader.ReadLine()
		if err != nil {
			return err
		}
		if !strings.HasPrefix(cmd, prefix) {
			return fmt.Errorf("expected SMTP command %q, got %q", prefix, cmd)
		}
		return send(reply)
	}
	if err := send("220 localhost"); err != nil {
		return err
	}
	for _, pair := range [][2]string{
		{"EHLO ", "250 localhost"},
		{"MAIL FROM:", "250 Sender OK"},
		{"RCPT TO:", "250 Recipient OK"},
		{"DATA", "354 Send message"},
	} {
		if err := exchange(pair[0], pair[1]); err != nil {
			return err
		}
	}
	body, err := reader.ReadDotBytes()
	if err != nil {
		return err
	}
	if !strings.Contains(string(body), "A pull-slip message") {
		return fmt.Errorf("SMTP DATA did not contain the message")
	}
	if dataResponse == "" {
		return nil
	}
	if err = send(dataResponse); err != nil {
		return err
	}
	if quitResponse != "" {
		return exchange("QUIT", quitResponse)
	}
	return nil
}
