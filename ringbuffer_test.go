package ringbuffer

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestRingBufferBasic(t *testing.T) {
	// Create a new ring buffer
	rb, err := NewRingBuffer("/tmp/test_rb.mmap", 1024, true)
	if err != nil {
		t.Fatalf("Failed to create ring buffer: %v", err)
	}
	defer rb.Close()
	defer os.Remove("/tmp/test_rb.mmap")

	// Test basic write and read
	testMsg := []byte("hello, ring buffer!")
	ok, err := rb.WriteMsg(testMsg)
	if !ok || err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	readMsg, err := rb.ReadMsg()
	if err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}

	if string(readMsg) != string(testMsg) {
		t.Errorf("Read message doesn't match written message. Got: %s, Want: %s", string(readMsg), string(testMsg))
	}
}

func TestRingBufferEmpty(t *testing.T) {
	rb, err := NewRingBuffer("/tmp/test_rb_empty.mmap", 1024, true)
	if err != nil {
		t.Fatalf("Failed to create ring buffer: %v", err)
	}
	defer rb.Close()
	defer os.Remove("/tmp/test_rb_empty.mmap")

	// Try to read from empty buffer
	_, err = rb.ReadMsg()
	if err != ErrBufferEmpty {
		t.Errorf("Expected ErrBufferEmpty, got: %v", err)
	}
}

func TestRingBufferInvalidSize(t *testing.T) {
	rb, err := NewRingBuffer("/tmp/test_rb_invalid.mmap", 1024, true)
	if err != nil {
		t.Fatalf("Failed to create ring buffer: %v", err)
	}
	defer rb.Close()
	defer os.Remove("/tmp/test_rb_invalid.mmap")

	// Test empty message
	ok, err := rb.WriteMsg([]byte{})
	if ok || err != ErrInvalidSize {
		t.Errorf("Expected ErrInvalidSize for empty message, got: %v", err)
	}

	// Test message too large
	largeMsg := make([]byte, 1024)
	ok, err = rb.WriteMsg(largeMsg)
	if ok || err != ErrInvalidSize {
		t.Errorf("Expected ErrInvalidSize for large message, got: %v", err)
	}
}

func TestRingBufferFull(t *testing.T) {
	// Create a small buffer to test full condition
	rb, err := NewRingBuffer("/tmp/test_rb_full.mmap", 256, true)
	if err != nil {
		t.Fatalf("Failed to create ring buffer: %v", err)
	}
	defer rb.Close()
	defer os.Remove("/tmp/test_rb_full.mmap")

	// Fill the buffer with small messages
	msg := []byte("test")
	count := 0
	for {
		ok, err := rb.WriteMsg(msg)
		if err == ErrBufferFull {
			break
		}
		if err != nil {
			t.Fatalf("Unexpected error while writing: %v", err)
		}
		if !ok {
			t.Fatalf("Write failed without error")
		}
		count++
		if count > 100 { // Safety limit
			t.Fatalf("Buffer not getting full after 100 writes")
		}
	}

	if count == 0 {
		t.Fatalf("Buffer became full without any successful writes")
	}
}

func TestRingBufferWrapAround(t *testing.T) {
	// Create a small buffer to force wrap-around
	rb, err := NewRingBuffer("/tmp/test_rb_wrap.mmap", 128, true)
	if err != nil {
		t.Fatalf("Failed to create ring buffer: %v", err)
	}
	defer rb.Close()
	defer os.Remove("/tmp/test_rb_wrap.mmap")

	// Write several messages to trigger wrap-around
	messages := []string{
		"first message",
		"second message is longer",
		"third",
		"fourth message",
		"fifth message to force wrap",
	}

	// Write all messages
	for i, msg := range messages {
		ok, err := rb.WriteMsg([]byte(msg))
		if !ok || err != nil {
			t.Fatalf("Failed to write message %d: %v", i, err)
		}
	}

	// Read all messages and verify
	for i, expectedMsg := range messages {
		readMsg, err := rb.ReadMsg()
		if err != nil {
			t.Fatalf("Failed to read message %d: %v", i, err)
		}
		if string(readMsg) != expectedMsg {
			t.Errorf("Message %d mismatch. Got: %s, Want: %s", i, string(readMsg), expectedMsg)
		}
	}

	// Buffer should be empty now
	_, err = rb.ReadMsg()
	if err != ErrBufferEmpty {
		t.Errorf("Expected ErrBufferEmpty after reading all messages, got: %v", err)
	}
}

func TestRingBufferConcurrentReadWrite(t *testing.T) {
	rb, err := NewRingBuffer("/tmp/test_rb_concurrent.mmap", 4096, true)
	if err != nil {
		t.Fatalf("Failed to create ring buffer: %v", err)
	}
	defer rb.Close()
	defer os.Remove("/tmp/test_rb_concurrent.mmap")

	const numMessages = 100
	const numWriters = 3
	const numReaders = 2

	// Channel to collect read messages and stop signal
	readMessages := make(chan string, numMessages*numWriters)
	stopReaders := make(chan struct{})

	// Start writers
	for w := 0; w < numWriters; w++ {
		go func(writerID int) {
			for i := 0; i < numMessages; i++ {
				msg := fmt.Sprintf("writer%d-msg%d", writerID, i)
				for {
					ok, err := rb.WriteMsg([]byte(msg))
					if ok && err == nil {
						break
					}
					if err != ErrBufferFull {
						t.Errorf("Unexpected write error: %v", err)
						return
					}
					// Buffer full, retry after small delay
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(w)
	}

	// Start readers
	for r := 0; r < numReaders; r++ {
		go func() {
			for {
				select {
				case <-stopReaders:
					return
				default:
					msg, err := rb.ReadMsg()
					if err == ErrBufferEmpty {
						time.Sleep(1 * time.Millisecond)
						continue
					}
					if err != nil {
						t.Errorf("Unexpected read error: %v", err)
						return
					}
					readMessages <- string(msg)
				}
			}
		}()
	}

	// Wait for all messages to be read
	expectedTotal := numMessages * numWriters
	readCount := 0
	timeout := time.After(10 * time.Second)

	for readCount < expectedTotal {
		select {
		case <-readMessages:
			readCount++
		case <-timeout:
			t.Fatalf("Timeout waiting for messages. Read %d/%d", readCount, expectedTotal)
		}
	}

	// Stop readers
	close(stopReaders)

	if readCount != expectedTotal {
		t.Errorf("Expected %d messages, got %d", expectedTotal, readCount)
	}
}

func TestRingBufferBoundaryConditions(t *testing.T) {
	// Test with minimum viable buffer size
	rb, err := NewRingBuffer("/tmp/test_rb_boundary.mmap", 32, true)
	if err != nil {
		t.Fatalf("Failed to create ring buffer: %v", err)
	}
	defer rb.Close()
	defer os.Remove("/tmp/test_rb_boundary.mmap")

	// Test message that exactly fits
	// For a 32-byte buffer: 8 bytes header + 4 bytes msg length + msg content
	// We need to account for the safety margin to distinguish full from empty
	availableSpace := 32 - 8 - 4 - 1 // buffer - header - msglen - safety margin
	maxMsg := make([]byte, availableSpace)
	for i := range maxMsg {
		maxMsg[i] = byte('A' + i%26)
	}

	ok, err := rb.WriteMsg(maxMsg)
	if !ok || err != nil {
		t.Fatalf("Failed to write max-size message: %v", err)
	}

	readMsg, err := rb.ReadMsg()
	if err != nil {
		t.Fatalf("Failed to read max-size message: %v", err)
	}

	if len(readMsg) != len(maxMsg) {
		t.Errorf("Message length mismatch. Got: %d, Want: %d", len(readMsg), len(maxMsg))
	}

	for i, b := range readMsg {
		if b != maxMsg[i] {
			t.Errorf("Message content mismatch at index %d. Got: %c, Want: %c", i, b, maxMsg[i])
		}
	}
}

func TestRingBufferMixedSizes(t *testing.T) {
	rb, err := NewRingBuffer("/tmp/test_rb_mixed.mmap", 1024, true)
	if err != nil {
		t.Fatalf("Failed to create ring buffer: %v", err)
	}
	defer rb.Close()
	defer os.Remove("/tmp/test_rb_mixed.mmap")

	// Create messages of various sizes
	messages := [][]byte{
		[]byte("a"),                     // 1 byte
		[]byte("hello"),                 // 5 bytes
		[]byte("medium length message"), // 21 bytes
		make([]byte, 100),               // 100 bytes
		[]byte("short"),                 // 5 bytes
		make([]byte, 200),               // 200 bytes
		[]byte("final"),                 // 5 bytes
	}

	// Fill large messages with pattern
	for i, msg := range messages {
		for j := range msg {
			msg[j] = byte('0' + (i+j)%10)
		}
	}

	// Write all messages
	for i, msg := range messages {
		ok, err := rb.WriteMsg(msg)
		if !ok || err != nil {
			t.Fatalf("Failed to write message %d: %v", i, err)
		}
	}

	// Read and verify all messages
	for i, expectedMsg := range messages {
		readMsg, err := rb.ReadMsg()
		if err != nil {
			t.Fatalf("Failed to read message %d: %v", i, err)
		}
		if len(readMsg) != len(expectedMsg) {
			t.Errorf("Message %d length mismatch. Got: %d, Want: %d", i, len(readMsg), len(expectedMsg))
		}
		for j, b := range readMsg {
			if b != expectedMsg[j] {
				t.Errorf("Message %d content mismatch at index %d. Got: %c, Want: %c", i, j, b, expectedMsg[j])
			}
		}
	}
}

func TestRingBufferReopen(t *testing.T) {
	filename := "/tmp/test_rb_reopen.mmap"

	// Create buffer and write some data
	rb1, err := NewRingBuffer(filename, 1024, true)
	if err != nil {
		t.Fatalf("Failed to create ring buffer: %v", err)
	}

	testMsg := []byte("persistent message")
	ok, err := rb1.WriteMsg(testMsg)
	if !ok || err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	rb1.Close()

	// Reopen and read the data
	rb2, err := OpenRingBuffer(filename)
	if err != nil {
		t.Fatalf("Failed to open ring buffer: %v", err)
	}
	defer rb2.Close()
	defer os.Remove(filename)

	readMsg, err := rb2.ReadMsg()
	if err != nil {
		t.Fatalf("Failed to read message after reopen: %v", err)
	}

	if string(readMsg) != string(testMsg) {
		t.Errorf("Message after reopen mismatch. Got: %s, Want: %s", string(readMsg), string(testMsg))
	}
}
