package ringbuffer

import (
	"encoding/binary"
	"errors"
	"os"
	"sync"
	"sync/atomic"
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
	buf        []byte
	size       int
	writeMu    sync.Mutex // Write lock
	writeCount uint64
	closed     bool
}

// NewRingBuffer creates a new mmap-backed ring buffer file
func NewRingBuffer(mmapFileName string, size int, remove bool) (*RingBuffer, error) {
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
	if msgLen == 0 || msgLen > uint32(r.size)/2 {
		return false, ErrInvalidSize
	}

	head, tail := r.GetHeadTail()
	size := uint32(r.size)

	// Calculate available space
	var free uint32
	if head >= tail {
		free = (size - head) + (tail - headerSize) - 1
	} else {
		free = tail - head - 1
	}

	need := 4 + msgLen
	if free <= need {
		return false, ErrBufferFull
	}

	// Write message length
	if head+4 > size-1 {
		head = headerSize
	}

	binary.LittleEndian.PutUint32(r.buf[head:head+4], msgLen)

	// Write message content
	writeEnd := head + 4 + msgLen
	if writeEnd < size-1 {
		copy(r.buf[head+4:writeEnd], msg)
	} else {
		firstPart := size - head - 4 - 1
		secondPart := msgLen - firstPart + 1
		if secondPart > 0 {
			copy(r.buf[head+4:size], msg[:firstPart])
			copy(r.buf[headerSize:headerSize+secondPart], msg[firstPart:])
		}
		writeEnd = headerSize + secondPart
	}

	// Update head pointer
	r.setHead(writeEnd)

	// Update write count
	atomic.AddUint64(&r.writeCount, 1)

	return true, nil
}

// ReadMsg reads a message from the ring buffer
// Returns (msg, nil) if successful, (nil, error) if failed
func (r *RingBuffer) ReadMsg() ([]byte, error) {
	if r.closed {
		return nil, ErrClosed
	}

	head, tail := r.GetHeadTail()
	if head == tail {
		return nil, ErrBufferEmpty
	}
	size := uint32(r.size)

	// Read message length
	if tail+4 > size-1 {
		tail = headerSize
	}

	msgLen := binary.LittleEndian.Uint32(r.buf[tail : tail+4])

	readEnd := tail + 4 + msgLen
	var msg []byte
	if readEnd < size-1 {
		msg = make([]byte, msgLen)
		copy(msg, r.buf[tail+4:readEnd])
	} else {
		firstPart := size - tail - 4 - 1
		secondPart := msgLen - firstPart + 1
		msg = make([]byte, msgLen)
		if secondPart > 0 {
			copy(msg[:firstPart], r.buf[tail+4:size-1])
			copy(msg[firstPart:], r.buf[headerSize:headerSize+secondPart])
		}
		readEnd = headerSize + secondPart
	}

	// Update tail pointer
	r.setTail(readEnd)

	return msg, nil
}

// Close releases mmap
func (r *RingBuffer) Close() error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
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
