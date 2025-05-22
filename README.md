# mmap_ringbuffer

A high-performance, memory-mapped ring buffer implementation in Go, designed for efficient inter-process or inter-thread communication. This library provides a lock-free (with minimal locking) ring buffer backed by memory-mapped files, making it suitable for high-throughput scenarios.

## Features

- Memory-mapped file as a shared ring buffer
- Minimal locking for write operations
- Efficient for high message throughput

## Installation

```bash
go get github.com/EBWi11/mmap_ringbuffer
```

## Usage

### Basic Example

```go
package main

import (
    "fmt"
    "github.com/EBWi11/mmap_ringbuffer"
)

func main() {
    // Create a new ring buffer with 1MB size
    rb, err := ringbuffer.NewRingBuffer("/tmp/rb.mmap", 1024*1024, true)
    if err != nil {
        panic(err)
    }
    defer rb.Close()

    // Write a message
    msg := []byte("hello, mmap ringbuffer!")
    ok, err := rb.WriteMsg(msg)
    if ok && err == nil {
        fmt.Println("Message written successfully")
    }

    // Read a message
    readMsg, err := rb.ReadMsg()
    if err == nil {
        fmt.Printf("Read message: %s\n", string(readMsg))
    }
}
```

### Producer-Consumer Example

```go
package main

import (
    "fmt"
    "sync"
    "github.com/EBWi11/mmap_ringbuffer"
)

func producer(rb *ringbuffer.RingBuffer, wg *sync.WaitGroup) {
    defer wg.Done()
    
    for i := 0; i < 1000; i++ {
        msg := []byte(fmt.Sprintf("message-%d", i))
        ok, err := rb.WriteMsg(msg)
        if !ok || err != nil {
            fmt.Printf("Failed to write: %v\n", err)
            return
        }
    }
}

func consumer(rb *ringbuffer.RingBuffer, wg *sync.WaitGroup) {
    defer wg.Done()
    
    for {
        msg, err := rb.ReadMsg()
        if err == ringbuffer.ErrBufferEmpty {
            continue
        }
        if err != nil {
            fmt.Printf("Failed to read: %v\n", err)
            return
        }
        fmt.Printf("Received: %s\n", string(msg))
    }
}

func main() {
    rb, err := ringbuffer.NewRingBuffer("/tmp/rb.mmap", 1024*1024, true)
    if err != nil {
        panic(err)
    }
    defer rb.Close()

    var wg sync.WaitGroup
    wg.Add(2)

    go producer(rb, &wg)
    go consumer(rb, &wg)

    wg.Wait()
}
```

## API Reference

### NewRingBuffer

```go
func NewRingBuffer(filename string, size int, remove bool) (*RingBuffer, error)
```

Creates a new ring buffer backed by a memory-mapped file.
- `filename`: Path to the memory-mapped file
- `size`: Size of the buffer in bytes
- `remove`: If true, removes any existing file before creating

### OpenRingBuffer

```go
func OpenRingBuffer(filename string) (*RingBuffer, error)
```

Opens an existing ring buffer file.

### WriteMsg

```go
func (r *RingBuffer) WriteMsg(msg []byte) (bool, error)
```

Writes a message to the buffer. Returns `true` and `nil` if successful.
- Returns `ErrBufferFull` if the buffer is full
- Returns `ErrInvalidSize` if the message is empty or too large
- Returns `ErrClosed` if the buffer is closed

### ReadMsg

```go
func (r *RingBuffer) ReadMsg() ([]byte, error)
```

Reads a message from the buffer. Returns the message and `nil` if successful.
- Returns `ErrBufferEmpty` if the buffer is empty
- Returns `ErrClosed` if the buffer is closed

### Close

```go
func (r *RingBuffer) Close() error
```

Closes the ring buffer and releases the memory-mapped file.

## Error Types

- `ErrBufferFull`: Returned when trying to write to a full buffer
- `ErrInvalidSize`: Returned when trying to write an empty or too large message
- `ErrBufferEmpty`: Returned when trying to read from an empty buffer
- `ErrClosed`: Returned when trying to use a closed buffer

## Performance Considerations

- The buffer size should be chosen carefully based on your use case
- For high-throughput scenarios, consider using a larger buffer size
- The buffer uses a header of 8 bytes (4 bytes for head, 4 bytes for tail)
- Maximum message size is limited to half of the buffer size

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
