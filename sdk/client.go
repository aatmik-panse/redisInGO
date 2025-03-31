package sdk

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	DefaultPort     = 7171
	DefaultTimeout  = 5 * time.Second
	MaxKeyLength    = 256
	MaxValueLength  = 256
	DefaultPoolSize = 10
)

// Command types
const (
	cmdPut byte = 1
	cmdGet byte = 2
)

// Response status
const (
	statusOK    byte = 1
	statusError byte = 2
)

// Common errors
var (
	ErrKeyNotFound      = errors.New("key not found")
	ErrKeyTooLarge      = errors.New("key exceeds maximum length")
	ErrValueTooLarge    = errors.New("value exceeds maximum length")
	ErrConnectionFailed = errors.New("failed to connect to server")
	ErrTimeout          = errors.New("operation timed out")
	ErrPoolExhausted    = errors.New("connection pool exhausted")
)

// ClientOptions holds configuration options for the Client
type ClientOptions struct {
	Port           int
	PoolSize       int
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
}

// DefaultClientOptions returns the default client options
func DefaultClientOptions() *ClientOptions {
	return &ClientOptions{
		Port:           DefaultPort,
		PoolSize:       DefaultPoolSize,
		ConnectTimeout: DefaultTimeout,
		ReadTimeout:    DefaultTimeout,
		WriteTimeout:   DefaultTimeout,
	}
}

type connection struct {
	conn   net.Conn
	reader *bufio.Reader
}

// Client represents a client for the Redis-like cache service
type Client struct {
	hostname string
	port     int
	options  *ClientOptions
	connPool chan *connection
	mu       sync.Mutex
}

// NewClient creates a new client connected to the specified hostname
func NewClient(hostname string, options *ClientOptions) *Client {
	if options == nil {
		options = DefaultClientOptions()
	}

	client := &Client{
		hostname: hostname,
		port:     options.Port,
		options:  options,
		connPool: make(chan *connection, options.PoolSize),
	}

	// Initialize connection pool with nil connections
	for i := 0; i < options.PoolSize; i++ {
		client.connPool <- nil
	}

	return client
}

// GetServerAddress returns the full server address (hostname:port)
func (c *Client) GetServerAddress() string {
	return fmt.Sprintf("%s:%d", c.hostname, c.port)
}

// Put stores a key-value pair in the cache
func (c *Client) Put(key, value string) error {
	if len(key) == 0 {
		return errors.New("key cannot be empty")
	}
	if len(key) > MaxKeyLength {
		return ErrKeyTooLarge
	}
	if len(value) > MaxValueLength {
		return ErrValueTooLarge
	}

	conn, err := c.getConnection()
	if err != nil {
		return err
	}
	defer c.releaseConnection(conn)

	// Set write deadline
	conn.conn.SetWriteDeadline(time.Now().Add(c.options.WriteTimeout))

	// Prepare PUT request
	req := new(bytes.Buffer)
	req.WriteByte(cmdPut)

	// Write key length (2 bytes)
	keyLenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(keyLenBytes, uint16(len(key)))
	req.Write(keyLenBytes)

	// Write value length (2 bytes)
	valueLenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(valueLenBytes, uint16(len(value)))
	req.Write(valueLenBytes)

	// Write key and value bytes
	req.WriteString(key)
	req.WriteString(value)

	// Send request
	if _, err := conn.conn.Write(req.Bytes()); err != nil {
		return fmt.Errorf("write error: %w", err)
	}

	// Set read deadline and read response
	conn.conn.SetReadDeadline(time.Now().Add(c.options.ReadTimeout))

	statusByte, err := conn.reader.ReadByte()
	if err != nil {
		return fmt.Errorf("read error: %w", err)
	}

	if statusByte != statusOK {
		// Read error message
		msgLenBytes := make([]byte, 2)
		if _, err := io.ReadFull(conn.reader, msgLenBytes); err != nil {
			return fmt.Errorf("failed to read error message length: %w", err)
		}

		msgLen := binary.BigEndian.Uint16(msgLenBytes)
		if msgLen > 1024 { // Sanity check
			return errors.New("invalid error message length")
		}

		msgBytes := make([]byte, msgLen)
		if _, err := io.ReadFull(conn.reader, msgBytes); err != nil {
			return fmt.Errorf("failed to read error message: %w", err)
		}

		return errors.New(string(msgBytes))
	}

	return nil
}

// Get retrieves a value for the specified key from the cache
func (c *Client) Get(key string) (string, error) {
	if len(key) == 0 {
		return "", errors.New("key cannot be empty")
	}
	if len(key) > MaxKeyLength {
		return "", ErrKeyTooLarge
	}

	conn, err := c.getConnection()
	if err != nil {
		return "", err
	}
	defer c.releaseConnection(conn)

	// Set write deadline
	conn.conn.SetWriteDeadline(time.Now().Add(c.options.WriteTimeout))

	// Prepare GET request
	req := new(bytes.Buffer)
	req.WriteByte(cmdGet)

	// Write key length (2 bytes)
	keyLenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(keyLenBytes, uint16(len(key)))
	req.Write(keyLenBytes)

	// Write key bytes
	req.WriteString(key)

	// Send request
	if _, err := conn.conn.Write(req.Bytes()); err != nil {
		return "", fmt.Errorf("write error: %w", err)
	}

	// Set read deadline and read response
	conn.conn.SetReadDeadline(time.Now().Add(c.options.ReadTimeout))

	statusByte, err := conn.reader.ReadByte()
	if err != nil {
		return "", fmt.Errorf("read error: %w", err)
	}

	if statusByte != statusOK {
		// Read error message
		msgLenBytes := make([]byte, 2)
		if _, err := io.ReadFull(conn.reader, msgLenBytes); err != nil {
			return "", fmt.Errorf("failed to read error message length: %w", err)
		}

		msgLen := binary.BigEndian.Uint16(msgLenBytes)
		if msgLen > 1024 { // Sanity check
			return "", errors.New("invalid error message length")
		}

		msgBytes := make([]byte, msgLen)
		if _, err := io.ReadFull(conn.reader, msgBytes); err != nil {
			return "", fmt.Errorf("failed to read error message: %w", err)
		}

		errMsg := string(msgBytes)
		if errMsg == "Key not found" {
			return "", ErrKeyNotFound
		}
		return "", errors.New(errMsg)
	}

	// Read value length
	valueLenBytes := make([]byte, 2)
	if _, err := io.ReadFull(conn.reader, valueLenBytes); err != nil {
		return "", fmt.Errorf("failed to read value length: %w", err)
	}

	valueLen := binary.BigEndian.Uint16(valueLenBytes)
	if valueLen > MaxValueLength {
		return "", errors.New("value length exceeds maximum")
	}

	// Read value
	valueBytes := make([]byte, valueLen)
	if _, err := io.ReadFull(conn.reader, valueBytes); err != nil {
		return "", fmt.Errorf("failed to read value: %w", err)
	}

	return string(valueBytes), nil
}

// Close closes all connections in the pool
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Close all connections in the pool
	for i := 0; i < c.options.PoolSize; i++ {
		select {
		case conn := <-c.connPool:
			if conn != nil {
				conn.conn.Close()
			}
		default:
			// Pool is empty
		}
	}

	// Close and recreate the channel
	close(c.connPool)
	c.connPool = make(chan *connection, c.options.PoolSize)
}

// getConnection gets a connection from the pool or creates a new one
func (c *Client) getConnection() (*connection, error) {
	var conn *connection

	// Get connection from pool
	select {
	case conn = <-c.connPool:
		// Got a connection (might be nil)
	default:
		return nil, ErrPoolExhausted
	}

	// Check if we need to create a new connection
	if conn == nil || !isConnectionValid(conn.conn) {
		// Create new connection
		netConn, err := net.DialTimeout("tcp", c.GetServerAddress(), c.options.ConnectTimeout)
		if err != nil {
			// Return connection to the pool and return error
			c.connPool <- nil
			return nil, fmt.Errorf("%w: %v", ErrConnectionFailed, err)
		}

		conn = &connection{
			conn:   netConn,
			reader: bufio.NewReader(netConn),
		}
	}

	return conn, nil
}

// releaseConnection returns a connection to the pool
func (c *Client) releaseConnection(conn *connection) {
	if conn == nil {
		c.connPool <- nil
		return
	}

	if !isConnectionValid(conn.conn) {
		if conn.conn != nil {
			conn.conn.Close()
		}
		c.connPool <- nil
		return
	}

	// Reset deadlines
	conn.conn.SetDeadline(time.Time{})

	// Reset reader
	conn.reader.Reset(conn.conn)

	// Return to pool
	select {
	case c.connPool <- conn:
		// Returned to pool
	default:
		// Pool is full, close connection
		conn.conn.Close()
	}
}

// isConnectionValid checks if a connection is still valid
func isConnectionValid(conn net.Conn) bool {
	if conn == nil {
		return false
	}

	// Set a very short deadline to check if the connection is alive
	err := conn.SetReadDeadline(time.Now().Add(time.Millisecond))
	if err != nil {
		return false
	}

	// Try to read 1 byte (this shouldn't succeed if the connection is good)
	one := make([]byte, 1)
	if _, err := conn.Read(one); err == io.EOF {
		// EOF means the connection is closed
		return false
	}

	// Reset deadline
	conn.SetReadDeadline(time.Time{})
	return true
}
