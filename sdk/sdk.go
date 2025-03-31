package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

const (
	cmdPut byte = 1
	cmdGet byte = 2

	statusOK    byte = 1
	statusError byte = 2

	maxLength = 256
)

// Client represents a client connection to the key-value cache server
type Client struct {
	addr           string
	connPool       chan *connection
	poolSize       int
	connTimeout    time.Duration
	requestTimeout time.Duration
	mu             sync.Mutex
}

type connection struct {
	conn   net.Conn
	reader *bufio.Reader
}

// NewClient creates a new client connection to the key-value cache server
func NewClient(addr string, options ...Option) *Client {
	// Default configuration
	c := &Client{
		addr:           addr,
		poolSize:       10,
		connTimeout:    5 * time.Second,
		requestTimeout: 3 * time.Second,
	}

	// Apply options
	for _, option := range options {
		option(c)
	}

	// Initialize connection pool
	c.connPool = make(chan *connection, c.poolSize)
	for i := 0; i < c.poolSize; i++ {
		c.connPool <- nil // Initialize with nil connections
	}

	return c
}

// Option represents a client option
type Option func(*Client)

// WithPoolSize sets the connection pool size
func WithPoolSize(size int) Option {
	return func(c *Client) {
		if size > 0 {
			c.poolSize = size
		}
	}
}

// WithConnTimeout sets the connection timeout
func WithConnTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.connTimeout = timeout
	}
}

// WithRequestTimeout sets the request timeout
func WithRequestTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.requestTimeout = timeout
	}
}

// Put inserts or updates a key-value pair in the cache
func (c *Client) Put(key, value string) bool {
	if len(key) > maxLength || len(value) > maxLength {
		return false
	}

	conn, err := c.getConn()
	if err != nil {
		return false
	}
	defer c.releaseConn(conn)

	// Create request buffer
	keyBytes := []byte(key)
	valueBytes := []byte(value)

	requestBuffer := &bytes.Buffer{}
	requestBuffer.WriteByte(cmdPut)

	// Write key length (2 bytes)
	keyLenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(keyLenBytes, uint16(len(keyBytes)))
	requestBuffer.Write(keyLenBytes)

	// Write value length (2 bytes)
	valueLenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(valueLenBytes, uint16(len(valueBytes)))
	requestBuffer.Write(valueLenBytes)

	// Write key and value
	requestBuffer.Write(keyBytes)
	requestBuffer.Write(valueBytes)

	// Set deadline for this request
	conn.conn.SetDeadline(time.Now().Add(c.requestTimeout))

	// Send request
	if _, err := conn.conn.Write(requestBuffer.Bytes()); err != nil {
		return false
	}

	// Read response
	statusByte, err := conn.reader.ReadByte()
	if err != nil {
		return false
	}

	return statusByte == statusOK
}

// Get retrieves a value from the cache
func (c *Client) Get(key string) (string, error) {
	if len(key) > maxLength {
		return "", errors.New("key too long")
	}

	conn, err := c.getConn()
	if err != nil {
		return "", err
	}
	defer c.releaseConn(conn)

	// Create request buffer
	keyBytes := []byte(key)

	requestBuffer := &bytes.Buffer{}
	requestBuffer.WriteByte(cmdGet)

	// Write key length (2 bytes)
	keyLenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(keyLenBytes, uint16(len(keyBytes)))
	requestBuffer.Write(keyLenBytes)

	// Write key
	requestBuffer.Write(keyBytes)

	// Set deadline for this request
	conn.conn.SetDeadline(time.Now().Add(c.requestTimeout))

	// Send request
	if _, err := conn.conn.Write(requestBuffer.Bytes()); err != nil {
		return "", err
	}

	// Read response
	statusByte, err := conn.reader.ReadByte()
	if err != nil {
		return "", err
	}

	if statusByte == statusError {
		// Read error message length
		msgLenBytes := make([]byte, 2)
		if _, err := io.ReadFull(conn.reader, msgLenBytes); err != nil {
			return "", err
		}
		msgLen := binary.BigEndian.Uint16(msgLenBytes)

		// Read error message
		msgBytes := make([]byte, msgLen)
		if _, err := io.ReadFull(conn.reader, msgBytes); err != nil {
			return "", err
		}

		if string(msgBytes) == "Key not found" {
			return "", nil // Return empty string for not found keys
		}

		return "", errors.New(string(msgBytes))
	}

	// Read value length
	valueLenBytes := make([]byte, 2)
	if _, err := io.ReadFull(conn.reader, valueLenBytes); err != nil {
		return "", err
	}
	valueLen := binary.BigEndian.Uint16(valueLenBytes)

	// Read value
	valueBytes := make([]byte, valueLen)
	if _, err := io.ReadFull(conn.reader, valueBytes); err != nil {
		return "", err
	}

	return string(valueBytes), nil
}

// Close closes all connections in the pool
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Drain connection pool and close connections
	for i := 0; i < c.poolSize; i++ {
		select {
		case conn := <-c.connPool:
			if conn != nil {
				conn.conn.Close()
			}
		default:
			// Pool is empty
		}
	}

	close(c.connPool)
}

// getConn gets a connection from the pool or creates a new one
func (c *Client) getConn() (*connection, error) {
	var conn *connection

	// Try to get connection from pool
	select {
	case conn = <-c.connPool:
		// Got a connection (might be nil)
	default:
		// Pool is empty, return error
		return nil, errors.New("connection pool exhausted")
	}

	// Check if connection is nil or closed
	if conn == nil || isConnClosed(conn.conn) {
		// Create new connection
		netConn, err := net.DialTimeout("tcp", c.addr, c.connTimeout)
		if err != nil {
			// Return failed connection to pool
			c.connPool <- nil
			return nil, err
		}

		conn = &connection{
			conn:   netConn,
			reader: bufio.NewReader(netConn),
		}
	}

	return conn, nil
}

// releaseConn returns a connection to the pool
func (c *Client) releaseConn(conn *connection) {
	// Check if connection is healthy
	if conn != nil && !isConnClosed(conn.conn) {
		// Reset reader buffer
		conn.reader.Reset(conn.conn)

		// Return to pool
		select {
		case c.connPool <- conn:
			// Successfully returned to pool
		default:
			// Pool is full, close connection
			conn.conn.Close()
		}
	} else {
		// Connection is unhealthy, return nil to pool
		select {
		case c.connPool <- nil:
			// Successfully returned nil to pool
		default:
			// Pool is full
		}
	}
}

// isConnClosed checks if a connection is closed
func isConnClosed(conn net.Conn) bool {
	if conn == nil {
		return true
	}

	// Set a very short deadline
	err := conn.SetReadDeadline(time.Now())
	if err != nil {
		return true
	}

	// Try to read 1 byte
	one := make([]byte, 1)
	_, err = conn.Read(one)

	// Reset deadline
	conn.SetReadDeadline(time.Time{})

	// Check if connection is closed
	if err == io.EOF {
		return true
	}

	// Not EOF, the connection might be still valid
	// Return the connection to its original state
	// by draining the buffer
	if operr, ok := err.(*net.OpError); ok && operr.Timeout() {
		// Timeout means the connection is still open but no data
		return false
	}

	// Any other error means the connection is probably bad
	return true
}
