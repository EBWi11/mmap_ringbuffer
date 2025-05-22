package ringbuffer

import (
	"os"
	"testing"
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
