package ringbuffer

import (
	"encoding/binary"
	"errors"
	"os"
	"sync"
	"syscall"
)

const (
	headerSize = 8 // 4 bytes for head, 4 bytes for tail
)

var (
	ErrBufferFull  = errors.New("ring buffer is full")
	ErrInvalidSize = errors.New("invalid message size")
	ErrBufferEmpty = errors.New("ring buffer is empty")
	ErrClosed      = errors.New("ring buffer is closed")
)

// RingBuffer implements a memory-mapped ring buffer.
// Memory layout:
// [magic(4)][head(4)][tail(4)][data...]
type RingBuffer struct {
	buf     []byte
	size    int
	writeMu sync.Mutex // Write lock
	readMu  sync.Mutex // Read lock
	closed  bool
}

// NewRingBuffer creates a new mmap-backed ring buffer file
func NewRingBuffer(mmapFileName string, size int, remove bool) (*RingBuffer, error) {
	if size <= headerSize {
		return nil, errors.New("buffer size must be larger than header size")
	}

	if remove {
		_ = os.Remove(mmapFileName)
	}

	file, err := os.OpenFile(mmapFileName, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Ensure file size is correct
	if err := file.Truncate(int64(size)); err != nil {
		return nil, err
	}

	buf, err := syscall.Mmap(int(file.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return nil, err
	}

	// Zero out the entire buffer
	for i := range buf {
		buf[i] = 0
	}

	rb := &RingBuffer{
		buf:  buf,
		size: size,
	}

	// Initialize the buffer
	rb.initialize()
	return rb, nil
}

func OpenRingBuffer(mmapFileName string) (*RingBuffer, error) {
	file, err := os.OpenFile(mmapFileName, os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fileInfo, err := os.Stat(mmapFileName)
	if err != nil {
		return nil, err
	}

	size := int(fileInfo.Size())

	buf, err := syscall.Mmap(int(file.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return nil, err
	}

	rb := &RingBuffer{
		buf:  buf,
		size: size,
	}

	return rb, nil
}

// initialize initializes the ring buffer
func (r *RingBuffer) initialize() {
	// Initialize head and tail
	r.setHead(headerSize)
	r.setTail(headerSize)
}

func (r *RingBuffer) GetHeadTail() (uint32, uint32) {
	return binary.LittleEndian.Uint32(r.buf[0:4]), binary.LittleEndian.Uint32(r.buf[4:8])
}

// setHead sets the head pointer
func (r *RingBuffer) setHead(val uint32) {
	binary.LittleEndian.PutUint32(r.buf[0:4], val)
}

// setTail sets the tail pointer
func (r *RingBuffer) setTail(val uint32) {
	binary.LittleEndian.PutUint32(r.buf[4:8], val)
}

// WriteMsg writes a message to the ring buffer
// Returns (true, nil) if successful, (false, error) if failed
func (r *RingBuffer) WriteMsg(msg []byte) (bool, error) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	if r.closed {
		return false, ErrClosed
	}

	msgLen := uint32(len(msg))
	if msgLen == 0 {
		return false, ErrInvalidSize
	}

	// Message should not be larger than buffer capacity minus header and length field
	maxMsgSize := uint32(r.size) - headerSize - 4
	if msgLen > maxMsgSize {
		return false, ErrInvalidSize
	}

	head, tail := r.GetHeadTail()
	size := uint32(r.size)

	// Calculate available space
	// We need to keep at least 1 byte difference between head and tail
	// to distinguish between empty and full buffer
	var free uint32
	if head >= tail {
		free = (size - head) + (tail - headerSize)
	} else {
		free = tail - head
	}

	need := 4 + msgLen
	if free <= need {
		return false, ErrBufferFull
	}

	// Check if we need to wrap around for the message length
	if head+4 > size {
		head = headerSize
		r.setHead(head)
	}

	// Write message length first
	binary.LittleEndian.PutUint32(r.buf[head:head+4], msgLen)

	// Calculate write position for message content
	writeStart := head + 4
	writeEnd := writeStart + msgLen

	// Handle wrap-around for message content
	if writeEnd < size {
		// No wrap-around needed
		copy(r.buf[writeStart:writeEnd], msg)
	} else {
		// Wrap-around needed
		firstPart := size - writeStart
		copy(r.buf[writeStart:size], msg[:firstPart])
		copy(r.buf[headerSize:headerSize+msgLen-firstPart], msg[firstPart:])
		writeEnd = headerSize + msgLen - firstPart
	}

	// Update head pointer
	r.setHead(writeEnd)
	return true, nil
}

// ReadMsg reads a message from the ring buffer
// Returns (msg, nil) if successful, (nil, error) if failed
func (r *RingBuffer) ReadMsg() ([]byte, error) {
	r.readMu.Lock()
	defer r.readMu.Unlock()

	if r.closed {
		return nil, ErrClosed
	}

	head, tail := r.GetHeadTail()
	if head == tail {
		return nil, ErrBufferEmpty
	}
	size := uint32(r.size)

	// Check if we need to wrap around for the message length
	if tail+4 > size {
		tail = headerSize
		r.setTail(tail)
	}

	// Read message length
	msgLen := binary.LittleEndian.Uint32(r.buf[tail : tail+4])

	// Calculate read position for message content
	readStart := tail + 4
	readEnd := readStart + msgLen
	msg := make([]byte, msgLen)

	// Handle wrap-around for message content
	if readEnd < size {
		// No wrap-around needed
		copy(msg, r.buf[readStart:readEnd])
	} else {
		// Wrap-around needed
		firstPart := size - readStart
		copy(msg[:firstPart], r.buf[readStart:size])
		copy(msg[firstPart:], r.buf[headerSize:headerSize+msgLen-firstPart])
		readEnd = headerSize + msgLen - firstPart
	}

	// Update tail pointer
	r.setTail(readEnd)
	return msg, nil
}

// Close releases mmap
func (r *RingBuffer) Close() error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	r.readMu.Lock()
	defer r.readMu.Unlock()
	if r.closed {
		return ErrClosed
	}
	r.closed = true
	var err error
	if r.buf != nil {
		err = syscall.Munmap(r.buf)
		r.buf = nil
	}
	return err
}
