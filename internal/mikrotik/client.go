// Package mikrotik mengimplementasikan klien protokol API RouterOS
// (port 8728 plaintext / 8729 TLS) tanpa dependensi eksternal.
package mikrotik

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

var ErrTrap = errors.New("routeros command failed")

type Client struct {
	connection net.Conn
	reader     *bufio.Reader
}

type DialOptions struct {
	Host     string
	Port     int
	UseTLS   bool
	Username string
	Password string
	Timeout  time.Duration
}

// Dial membuka koneksi, melakukan login (RouterOS 6.43+ plaintext login),
// dan mengembalikan klien siap pakai.
func Dial(ctx context.Context, options DialOptions) (*Client, error) {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	address := net.JoinHostPort(options.Host, fmt.Sprintf("%d", options.Port))
	dialer := &net.Dialer{Timeout: timeout}

	var connection net.Conn
	var err error
	if options.UseTLS {
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			// Router MikroTik umumnya memakai sertifikat self-signed pada API-SSL.
			Config: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
		connection, err = tlsDialer.DialContext(ctx, "tcp", address)
	} else {
		connection, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, fmt.Errorf("hubungi router %s: %w", address, err)
	}

	client := &Client{connection: connection, reader: bufio.NewReader(connection)}
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = connection.SetDeadline(deadline)

	if _, err := client.Run("/login", "=name="+options.Username, "=password="+options.Password); err != nil {
		client.Close()
		if errors.Is(err, ErrTrap) {
			return nil, fmt.Errorf("login router ditolak: %w", err)
		}
		return nil, fmt.Errorf("login router: %w", err)
	}
	return client, nil
}

func (client *Client) Close() {
	_ = client.connection.Close()
}

// Run mengirim satu kalimat perintah dan mengumpulkan seluruh balasan !re
// hingga !done. Balasan !trap menghasilkan error ErrTrap.
func (client *Client) Run(words ...string) ([]map[string]string, error) {
	for _, word := range words {
		if err := client.writeWord(word); err != nil {
			return nil, err
		}
	}
	if err := client.writeWord(""); err != nil {
		return nil, err
	}

	var results []map[string]string
	var trapMessage string
	for {
		sentence, err := client.readSentence()
		if err != nil {
			return nil, err
		}
		if len(sentence) == 0 {
			continue
		}
		switch sentence[0] {
		case "!re":
			results = append(results, parseAttributes(sentence[1:]))
		case "!trap", "!fatal":
			attributes := parseAttributes(sentence[1:])
			if attributes["message"] != "" {
				trapMessage = attributes["message"]
			} else {
				trapMessage = strings.Join(sentence[1:], " ")
			}
		case "!done":
			if trapMessage != "" {
				return nil, fmt.Errorf("%w: %s", ErrTrap, trapMessage)
			}
			return results, nil
		}
	}
}

func parseAttributes(words []string) map[string]string {
	attributes := make(map[string]string, len(words))
	for _, word := range words {
		if !strings.HasPrefix(word, "=") {
			continue
		}
		trimmed := strings.TrimPrefix(word, "=")
		key, value, _ := strings.Cut(trimmed, "=")
		attributes[key] = value
	}
	return attributes
}

func (client *Client) writeWord(word string) error {
	if err := client.writeLength(len(word)); err != nil {
		return err
	}
	_, err := client.connection.Write([]byte(word))
	return err
}

func (client *Client) writeLength(length int) error {
	var encoded []byte
	switch {
	case length < 0x80:
		encoded = []byte{byte(length)}
	case length < 0x4000:
		encoded = []byte{byte(length>>8) | 0x80, byte(length)}
	case length < 0x200000:
		encoded = []byte{byte(length>>16) | 0xC0, byte(length >> 8), byte(length)}
	case length < 0x10000000:
		encoded = []byte{byte(length>>24) | 0xE0, byte(length >> 16), byte(length >> 8), byte(length)}
	default:
		encoded = []byte{0xF0, byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length)}
	}
	_, err := client.connection.Write(encoded)
	return err
}

func (client *Client) readLength() (int, error) {
	first, err := client.reader.ReadByte()
	if err != nil {
		return 0, err
	}
	switch {
	case first < 0x80:
		return int(first), nil
	case first < 0xC0:
		rest, err := client.readBytes(1)
		if err != nil {
			return 0, err
		}
		return (int(first&^0x80) << 8) | int(rest[0]), nil
	case first < 0xE0:
		rest, err := client.readBytes(2)
		if err != nil {
			return 0, err
		}
		return (int(first&^0xC0) << 16) | int(rest[0])<<8 | int(rest[1]), nil
	case first < 0xF0:
		rest, err := client.readBytes(3)
		if err != nil {
			return 0, err
		}
		return (int(first&^0xE0) << 24) | int(rest[0])<<16 | int(rest[1])<<8 | int(rest[2]), nil
	default:
		rest, err := client.readBytes(4)
		if err != nil {
			return 0, err
		}
		return int(rest[0])<<24 | int(rest[1])<<16 | int(rest[2])<<8 | int(rest[3]), nil
	}
}

func (client *Client) readBytes(count int) ([]byte, error) {
	buffer := make([]byte, count)
	for offset := 0; offset < count; {
		read, err := client.reader.Read(buffer[offset:])
		if err != nil {
			return nil, err
		}
		offset += read
	}
	return buffer, nil
}

func (client *Client) readSentence() ([]string, error) {
	var sentence []string
	for {
		length, err := client.readLength()
		if err != nil {
			return nil, err
		}
		if length == 0 {
			return sentence, nil
		}
		word, err := client.readBytes(length)
		if err != nil {
			return nil, err
		}
		sentence = append(sentence, string(word))
	}
}
