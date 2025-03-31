package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	cmdPut byte = 1
	cmdGet byte = 2
)

const (
	statusOK    byte = 1
	statusError byte = 2
)

var (
	host        = flag.String("host", "localhost", "Server host")
	port        = flag.Int("port", 7171, "Server port")
	duration    = flag.Int("duration", 30, "Test duration in seconds")
	connections = flag.Int("connections", 5000, "Number of concurrent connections")
	keySize     = flag.Int("keysize", 4, "Key size in bytes")
	valueSize   = flag.Int("valuesize", 32, "Value size in bytes")
	ratio       = flag.Int("ratio", 2, "GET/PUT ratio (e.g., 3 means 3 GETs for each PUT)")
	maxRetries  = flag.Int("retries", 3, "Maximum connection retry attempts")
	verbosity   = flag.Int("verbosity", 1, "Error logging verbosity (0=minimal, 1=normal, 2=verbose)")
	logFile     = flag.String("logfile", "", "Log file path (empty=stdout)")
)

// Logger setup
var logger *log.Logger

// Error log levels
const (
	errorCritical = iota
	errorWarning
	errorInfo
	errorDebug
)

func initLogger() {
	var output io.Writer = os.Stdout
	if *logFile != "" {
		file, err := os.Create(*logFile)
		if err != nil {
			fmt.Printf("Error creating log file %s: %v, using stdout\n", *logFile, err)
		} else {
			output = file
		}
	}
	logger = log.New(output, "", log.LstdFlags)
	logger.Printf("Benchmark started with verbosity level %d\n", *verbosity)
}

func logError(level int, format string, args ...interface{}) {
	if level <= *verbosity {
		prefix := ""
		switch level {
		case errorCritical:
			prefix = "[CRITICAL] "
		case errorWarning:
			prefix = "[WARNING] "
		case errorInfo:
			prefix = "[INFO] "
		case errorDebug:
			prefix = "[DEBUG] "
		}
		logger.Printf(prefix+format, args...)
	}
}

type client struct {
	conn   net.Conn
	reader *bufio.Reader
	id     int // Client ID for better error tracking
}

func newClient(host string, port int, id int) (*client, error) {
	var conn net.Conn
	var err error

	addr := fmt.Sprintf("%s:%d", host, port)
	logError(errorDebug, "Client %d attempting to connect to %s", id, addr)

	// Add connection retry logic
	for i := 0; i < *maxRetries; i++ {
		conn, err = net.DialTimeout("tcp", addr, 5*time.Second)
		if err == nil {
			logError(errorDebug, "Client %d successfully connected after %d attempts", id, i+1)
			break
		}

		if i < *maxRetries-1 {
			// Wait before retrying (exponential backoff)
			backoff := time.Duration(100*(i+1)) * time.Millisecond
			logError(errorWarning, "Client %d connection attempt %d failed: %v, retrying in %v",
				id, i+1, err, backoff)
			time.Sleep(backoff)
		} else {
			logError(errorCritical, "Client %d failed all connection attempts: %v", id, err)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("client %d failed after %d attempts: %v", id, *maxRetries, err)
	}

	return &client{
		conn:   conn,
		reader: bufio.NewReader(conn),
		id:     id,
	}, nil
}

func (c *client) close() {
	logError(errorDebug, "Client %d closing connection", c.id)
	c.conn.Close()
}

func (c *client) put(key, value []byte) error {
	// Validate key and value sizes
	if len(key) > 255 || len(value) > 255 {
		logError(errorWarning, "Client %d PUT invalid sizes - key: %d bytes, value: %d bytes",
			c.id, len(key), len(value))
		return fmt.Errorf("key or value size exceeds maximum allowed")
	}

	// Command byte
	if _, err := c.conn.Write([]byte{cmdPut}); err != nil {
		logError(errorWarning, "Client %d PUT command write failed: %v", c.id, err)
		return err
	}

	// Key length (2 bytes)
	keyLenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(keyLenBytes, uint16(len(key)))
	if _, err := c.conn.Write(keyLenBytes); err != nil {
		logError(errorWarning, "Client %d PUT key length write failed: %v", c.id, err)
		return err
	}

	// Value length (2 bytes)
	valueLenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(valueLenBytes, uint16(len(value)))
	if _, err := c.conn.Write(valueLenBytes); err != nil {
		logError(errorWarning, "Client %d PUT value length write failed: %v", c.id, err)
		return err
	}

	// Log the full packet for detailed debugging when needed
	logError(errorDebug, "Client %d PUT packet: cmd=%d, keyLen=%d, valueLen=%d",
		c.id, cmdPut, len(key), len(value))

	// Key
	if _, err := c.conn.Write(key); err != nil {
		logError(errorWarning, "Client %d PUT key write failed: %v", c.id, err)
		return err
	}

	// Value
	if _, err := c.conn.Write(value); err != nil {
		logError(errorWarning, "Client %d PUT value write failed: %v", c.id, err)
		return err
	}

	// Read response with timeout to prevent hanging
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer c.conn.SetReadDeadline(time.Time{}) // Reset deadline

	statusByte, err := c.reader.ReadByte()
	if err != nil {
		logError(errorWarning, "Client %d PUT response read failed: %v", c.id, err)
		return err
	}

	if statusByte != statusOK {
		// Read error message
		msgLenBytes := make([]byte, 2)
		if _, err := io.ReadFull(c.reader, msgLenBytes); err != nil {
			logError(errorWarning, "Client %d PUT error message length read failed: %v", c.id, err)
			return err
		}
		msgLen := binary.BigEndian.Uint16(msgLenBytes)

		// Validate message length to prevent buffer overflows
		if msgLen > 1024 {
			logError(errorWarning, "Client %d PUT received abnormal error message length: %d", c.id, msgLen)
			return fmt.Errorf("abnormal error message length: %d", msgLen)
		}

		msgBytes := make([]byte, msgLen)
		if _, err := io.ReadFull(c.reader, msgBytes); err != nil {
			logError(errorWarning, "Client %d PUT error message read failed: %v", c.id, err)
			return err
		}

		errMsg := string(msgBytes)
		logError(errorWarning, "Client %d PUT server error: %s", c.id, errMsg)

		// Retry logic for certain error types
		if strings.Contains(errMsg, "cache full") {
			logError(errorInfo, "Client %d PUT failed due to cache full, will retry later", c.id)
			time.Sleep(100 * time.Millisecond) // Back off before retrying
			return fmt.Errorf("temporarily unavailable: %s", errMsg)
		}

		return fmt.Errorf("server error: %s", errMsg)
	}

	return nil
}

func (c *client) get(key []byte) ([]byte, error) {
	// Validate key size
	if len(key) > 255 {
		logError(errorWarning, "Client %d GET invalid key size: %d bytes",
			c.id, len(key))
		return nil, fmt.Errorf("key size exceeds maximum allowed")
	}

	// Command byte
	if _, err := c.conn.Write([]byte{cmdGet}); err != nil {
		logError(errorWarning, "Client %d GET command write failed: %v", c.id, err)
		return nil, err
	}

	// Key length (2 bytes)
	keyLenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(keyLenBytes, uint16(len(key)))
	if _, err := c.conn.Write(keyLenBytes); err != nil {
		logError(errorWarning, "Client %d GET key length write failed: %v", c.id, err)
		return nil, err
	}

	// Log the full packet for detailed debugging when needed
	logError(errorDebug, "Client %d GET packet: cmd=%d, keyLen=%d",
		c.id, cmdGet, len(key))

	// Key
	if _, err := c.conn.Write(key); err != nil {
		logError(errorWarning, "Client %d GET key write failed: %v", c.id, err)
		return nil, err
	}

	// Read response
	statusByte, err := c.reader.ReadByte()
	if err != nil {
		logError(errorWarning, "Client %d GET response read failed: %v", c.id, err)
		return nil, err
	}

	if statusByte != statusOK {
		// Read error message
		msgLenBytes := make([]byte, 2)
		if _, err := io.ReadFull(c.reader, msgLenBytes); err != nil {
			logError(errorWarning, "Client %d GET error message length read failed: %v", c.id, err)
			return nil, err
		}
		msgLen := binary.BigEndian.Uint16(msgLenBytes)

		// Validate message length to prevent buffer overflows
		if msgLen > 1024 {
			logError(errorWarning, "Client %d GET received abnormal error message length: %d", c.id, msgLen)
			return nil, fmt.Errorf("abnormal error message length: %d", msgLen)
		}

		msgBytes := make([]byte, msgLen)
		if _, err := io.ReadFull(c.reader, msgBytes); err != nil {
			logError(errorWarning, "Client %d GET error message read failed: %v", c.id, err)
			return nil, err
		}

		errMsg := string(msgBytes)
		logError(errorWarning, "Client %d GET server error: %s", c.id, errMsg)
		return nil, fmt.Errorf("server error: %s", errMsg)
	}

	// Read value length (2 bytes)
	valueLenBytes := make([]byte, 2)
	if _, err := io.ReadFull(c.reader, valueLenBytes); err != nil {
		logError(errorWarning, "Client %d GET value length read failed: %v", c.id, err)
		return nil, err
	}
	valueLen := binary.BigEndian.Uint16(valueLenBytes)

	// Validate value length to prevent buffer overflows
	if valueLen > 8192 {
		logError(errorWarning, "Client %d GET received abnormal value length: %d", c.id, valueLen)
		return nil, fmt.Errorf("abnormal value length: %d", valueLen)
	}

	// Read value
	valueBytes := make([]byte, valueLen)
	if _, err := io.ReadFull(c.reader, valueBytes); err != nil {
		logError(errorWarning, "Client %d GET value read failed: %v", c.id, err)
		return nil, err
	}

	return valueBytes, nil
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

type responseStats struct {
	totalTime int64
	count     uint64
}

func worker(
	host string,
	port int,
	keySize int,
	valueSize int,
	ratio int,
	wg *sync.WaitGroup,
	done chan struct{},
	successCount *uint64,
	errorCount *uint64,
	totalResponseTime *int64,
	workerID int, // Added worker ID for better tracking
) {
	defer wg.Done()

	logError(errorInfo, "Worker %d starting", workerID)

	// Create client with retry logic
	var c *client
	var err error
	for i := 0; i < *maxRetries; i++ {
		c, err = newClient(host, port, workerID)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(100*(i+1)) * time.Millisecond)
	}

	if err != nil {
		logError(errorCritical, "Worker %d failed to create client after %d retries: %v",
			workerID, *maxRetries, err)
		atomic.AddUint64(errorCount, 1)
		return
	}
	defer c.close()

	// Use smaller key size if configured value is too large
	if keySize > 250 {
		keySize = 250
		logError(errorWarning, "Worker %d reducing key size to %d bytes to avoid protocol limits",
			workerID, keySize)
	}

	// Use smaller value size if configured value is too large
	if valueSize > 250 {
		valueSize = 250
		logError(errorWarning, "Worker %d reducing value size to %d bytes to avoid protocol limits",
			workerID, valueSize)
	}

	// Create a set of keys to use - reduce from 1000 to avoid initial connection issues
	keysCount := 100
	keys := make([][]byte, keysCount)
	for i := range keys {
		keys[i] = randomBytes(keySize)
	}

	logError(errorDebug, "Worker %d generated %d random keys of size %d", workerID, keysCount, keySize)

	// Store some values first (with error handling and retry logic)
	preloadErrors := 0
	keysLoaded := 0
	for i := 0; i < keysCount && keysLoaded < 10; i++ {
		value := randomBytes(valueSize)
		retries := 3

		for r := 0; r < retries; r++ {
			err := c.put(keys[i], value)
			if err == nil {
				keysLoaded++
				atomic.AddUint64(successCount, 1)
				break
			} else if r == retries-1 {
				preloadErrors++
				atomic.AddUint64(errorCount, 1)
				logError(errorWarning, "Worker %d preload error on key %d: %v", workerID, i, err)
			} else if strings.Contains(err.Error(), "temporarily unavailable") {
				// Server is under memory pressure, wait longer
				time.Sleep(500 * time.Millisecond)
			} else {
				// Other errors, shorter wait
				time.Sleep(50 * time.Millisecond)
			}
		}

		// If we're having persistent issues in preloading, just continue with what we have
		if i > 10 && preloadErrors > i/2 {
			logError(errorWarning, "Worker %d excessive preload errors (%d/%d), continuing with %d keys",
				workerID, preloadErrors, i+1, keysLoaded)
			break
		}
	}

	logError(errorInfo, "Worker %d completed preloading with %d/%d errors, loaded %d keys",
		workerID, preloadErrors, keysCount, keysLoaded)

	// Run benchmark loop
	counter := 0
	opErrors := 0
	opSuccess := 0

	logError(errorInfo, "Worker %d starting benchmark loop", workerID)

	for {
		select {
		case <-done:
			logError(errorInfo, "Worker %d finished with %d successful ops and %d errors",
				workerID, opSuccess, opErrors)
			return
		default:
			// Decide whether to PUT or GET based on ratio
			if counter%ratio == 0 {
				// PUT
				key := keys[rand.Intn(len(keys))]
				value := randomBytes(valueSize)

				start := time.Now()
				err := c.put(key, value)
				elapsed := time.Since(start).Microseconds()
				atomic.AddInt64(totalResponseTime, elapsed)

				if err != nil {
					opErrors++
					atomic.AddUint64(errorCount, 1)
					if opErrors%100 == 0 {
						logError(errorWarning, "Worker %d accumulated %d PUT errors, last error: %v",
							workerID, opErrors, err)
					}
				} else {
					opSuccess++
					atomic.AddUint64(successCount, 1)
				}
			} else {
				// GET
				key := keys[rand.Intn(len(keys))]

				start := time.Now()
				_, err := c.get(key)
				elapsed := time.Since(start).Microseconds()
				atomic.AddInt64(totalResponseTime, elapsed)

				if err != nil {
					opErrors++
					atomic.AddUint64(errorCount, 1)
					if opErrors%100 == 0 {
						logError(errorWarning, "Worker %d accumulated %d GET errors, last error: %v",
							workerID, opErrors, err)
					}
				} else {
					opSuccess++
					atomic.AddUint64(successCount, 1)
				}
			}

			counter++

			// Periodic worker status reporting
			if counter%10000 == 0 {
				logError(errorDebug, "Worker %d processed %d operations (%d successful, %d errors)",
					workerID, counter, opSuccess, opErrors)
			}
		}
	}
}

// Add additional function to monitor and diagnose protocol issues
func addProtocolDiagnostics() {
	// Create a separate diagnostic client that logs all raw traffic
	go func() {
		logError(errorInfo, "Starting protocol diagnostics client")

		time.Sleep(5 * time.Second) // Wait for server to be ready

		c, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", *host, *port), 5*time.Second)
		if err != nil {
			logError(errorCritical, "Diagnostics client failed to connect: %v", err)
			return
		}
		defer c.Close()

		reader := bufio.NewReader(c)

		// Test basic protocol with instrumentation
		logError(errorInfo, "Diagnostics: Testing PUT command")

		// Create test data
		testKey := []byte("test-key-12345")
		testValue := []byte("test-value-12345")

		// Log raw bytes being sent
		cmdBytes := []byte{cmdPut}
		keyLenBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(keyLenBytes, uint16(len(testKey)))
		valueLenBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(valueLenBytes, uint16(len(testValue)))

		logError(errorInfo, "Diagnostics: Sending PUT - cmd: %v, keyLen: %v, valueLen: %v, key: %s, value: %s",
			cmdBytes, keyLenBytes, valueLenBytes, testKey, testValue)

		// Send command
		c.Write(cmdBytes)
		c.Write(keyLenBytes)
		c.Write(valueLenBytes)
		c.Write(testKey)
		c.Write(testValue)

		// Read response
		statusByte, err := reader.ReadByte()
		if err != nil {
			logError(errorCritical, "Diagnostics: Failed to read PUT response: %v", err)
			return
		}

		logError(errorInfo, "Diagnostics: PUT response status: %d", statusByte)

		if statusByte != statusOK {
			// Read error message
			msgLenBytes := make([]byte, 2)
			if _, err := io.ReadFull(reader, msgLenBytes); err != nil {
				logError(errorCritical, "Diagnostics: Failed to read error length: %v", err)
				return
			}
			msgLen := binary.BigEndian.Uint16(msgLenBytes)

			msgBytes := make([]byte, msgLen)
			if _, err := io.ReadFull(reader, msgBytes); err != nil {
				logError(errorCritical, "Diagnostics: Failed to read error message: %v", err)
				return
			}

			logError(errorCritical, "Diagnostics: PUT failed with server error: %s", string(msgBytes))
		} else {
			logError(errorInfo, "Diagnostics: PUT succeeded")
		}

		// Test GET command
		logError(errorInfo, "Diagnostics: Testing GET command")

		cmdBytes = []byte{cmdGet}

		logError(errorInfo, "Diagnostics: Sending GET - cmd: %v, keyLen: %v, key: %s",
			cmdBytes, keyLenBytes, testKey)

		c.Write(cmdBytes)
		c.Write(keyLenBytes)
		c.Write(testKey)

		statusByte, err = reader.ReadByte()
		if err != nil {
			logError(errorCritical, "Diagnostics: Failed to read GET response: %v", err)
			return
		}

		logError(errorInfo, "Diagnostics: GET response status: %d", statusByte)

		if statusByte != statusOK {
			// Read error message
			msgLenBytes := make([]byte, 2)
			if _, err := io.ReadFull(reader, msgLenBytes); err != nil {
				logError(errorCritical, "Diagnostics: Failed to read error length: %v", err)
				return
			}
			msgLen := binary.BigEndian.Uint16(msgLenBytes)

			msgBytes := make([]byte, msgLen)
			if _, err := io.ReadFull(reader, msgBytes); err != nil {
				logError(errorCritical, "Diagnostics: Failed to read error message: %v", err)
				return
			}

			logError(errorCritical, "Diagnostics: GET failed with server error: %s", string(msgBytes))
		} else {
			// Read value length
			valueLenBytes := make([]byte, 2)
			if _, err := io.ReadFull(reader, valueLenBytes); err != nil {
				logError(errorCritical, "Diagnostics: Failed to read value length: %v", err)
				return
			}
			valueLen := binary.BigEndian.Uint16(valueLenBytes)

			// Read value
			valueBytes := make([]byte, valueLen)
			if _, err := io.ReadFull(reader, valueBytes); err != nil {
				logError(errorCritical, "Diagnostics: Failed to read value: %v", err)
				return
			}

			logError(errorInfo, "Diagnostics: GET succeeded, value: %s", string(valueBytes))
		}

		logError(errorInfo, "Protocol diagnostics completed")
	}()
}

func main() {
	flag.Parse()

	// Initialize logger
	initLogger()

	// Initialize protocol diagnostics
	addProtocolDiagnostics()

	logError(errorInfo, "=== Benchmark Configuration ===")
	logError(errorInfo, "Host: %s:%d", *host, *port)
	logError(errorInfo, "Duration: %d seconds", *duration)
	logError(errorInfo, "Connections: %d", *connections)
	logError(errorInfo, "Key size: %d bytes", *keySize)
	logError(errorInfo, "Value size: %d bytes", *valueSize)
	logError(errorInfo, "GET/PUT ratio: %d:1", *ratio)
	logError(errorInfo, "Max retries: %d", *maxRetries)

	fmt.Printf("Starting benchmark with %d connections for %d seconds\n", *connections, *duration)
	fmt.Printf("Key size: %d bytes, Value size: %d bytes, GET/PUT ratio: %d:1\n", *keySize, *valueSize, *ratio)
	fmt.Printf("Connecting to %s:%d with max %d retries\n", *host, *port, *maxRetries)

	// Initialize random seed with current time for better randomization
	rand.Seed(time.Now().UnixNano())

	// Limit initial connections to prevent overwhelming the server
	initialBatch := *connections
	if initialBatch > 1000 {
		initialBatch = 1000
		logError(errorInfo, "Limiting initial batch to %d connections", initialBatch)
	}

	// Limit connections further if using larger objects
	if *keySize > 100 || *valueSize > 1000 {
		initialMultiplier := 0.5
		reducedConnections := int(float64(*connections) * initialMultiplier)
		if reducedConnections < 100 {
			reducedConnections = 100
		}

		logError(errorInfo, "Large key/value sizes detected. Reducing connections from %d to %d to prevent server overload",
			*connections, reducedConnections)
		*connections = reducedConnections
	}

	// Setup counters
	var successCount, errorCount uint64
	var totalResponseTime int64

	// Create workers in batches to prevent connection flood
	var wg sync.WaitGroup
	done := make(chan struct{})

	fmt.Printf("Starting initial batch of %d connections...\n", initialBatch)
	logError(errorInfo, "Starting initial batch of %d connections", initialBatch)

	// Launch workers in smaller batches
	for i := 0; i < *connections; i++ {
		wg.Add(1)
		go worker(*host, *port, *keySize, *valueSize, *ratio, &wg, done,
			&successCount, &errorCount, &totalResponseTime, i+1)

		// Add small delay between connection attempts and pause after each batch
		if i < initialBatch {
			time.Sleep(time.Millisecond * 5)
			if i > 0 && i%100 == 0 {
				time.Sleep(time.Millisecond * 500)
				fmt.Printf("Started %d connections\n", i)
				logError(errorInfo, "Started %d connections", i)
			}
		} else {
			// For connections beyond initial batch, use a slightly different approach
			if i%500 == 0 {
				fmt.Printf("Started %d connections\n", i)
				logError(errorInfo, "Started %d connections", i)
				time.Sleep(time.Second) // Longer pause between large batches
			} else {
				time.Sleep(time.Millisecond) // Small delay between each connection
			}
		}
	}

	// Run benchmark for specified duration
	startTime := time.Now()
	logError(errorInfo, "All workers started, benchmark running for %d seconds", *duration)
	time.Sleep(time.Duration(*duration) * time.Second)
	logError(errorInfo, "Benchmark duration complete, stopping workers")
	close(done)

	// Wait for all workers to finish
	logError(errorInfo, "Waiting for all workers to finish")
	wg.Wait()
	logError(errorInfo, "All workers have finished")

	// Calculate results
	elapsed := time.Since(startTime).Seconds()
	totalOps := atomic.LoadUint64(&successCount) + atomic.LoadUint64(&errorCount)
	successOps := atomic.LoadUint64(&successCount)
	errorOps := atomic.LoadUint64(&errorCount)

	// Calculate RPS, failure rate and average response time
	rps := float64(totalOps) / elapsed
	failureRate := float64(errorOps) / float64(totalOps) * 100

	// Calculate average response time in microseconds
	var avgResponseTime float64 = 0
	if totalOps > 0 {
		avgResponseTime = float64(atomic.LoadInt64(&totalResponseTime)) / float64(totalOps)
	}

	resultsSummary := fmt.Sprintf("\n=== Benchmark Results ===\n"+
		"Total operations: %d\n"+
		"Successful operations: %d\n"+
		"Failed operations: %d\n"+
		"Throughput: %.2f ops/sec\n"+
		"Failure rate: %.2f%%\n"+
		"Average response time: %.2f microseconds\n"+
		"Total duration: %.2f seconds\n",
		totalOps, successOps, errorOps, rps, failureRate, avgResponseTime, elapsed)

	fmt.Print(resultsSummary)
	logError(errorInfo, resultsSummary)
}
