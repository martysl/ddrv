package discord

import (
    "io"
)

// Writer is a custom writer that implements io.WriteCloser.
// It streams data in chunks discord server channels using webhook
type Writer struct {
    archive   *Discord // Discord where Writer writes data
    chunks    []Chunk  // Chunks written so far
    chunkSize int      // The maximum Size of a chunk

    idx       int            // Current position in the current chunk
    closed    bool           // Whether the Writer has been closed
    errCh     chan error     // Channel to send any errors that occur during writing
    chunkCh   chan Chunk     // Channel to send chunks after they're written
    pwriter   *io.PipeWriter // PipeWriter for writing the current chunk
    streamErr error          // Last error occurred during stream write
}

// NewWriter creates a new Writer with the given chunk Size and storage.
func NewWriter(chunkSize int, arc *Discord) *Writer {
    sw := &Writer{
        archive:   arc,
        chunks:    make([]Chunk, 0),
        errCh:     make(chan error, 1),
        chunkCh:   make(chan Chunk, 1),
        chunkSize: chunkSize,
    }
    return sw
}

// Write implements the Write method of io.Writer. It writes p to the Writer.
// If p is larger than the remaining space in the current chunk, Write splits p across
// multiple chunks as needed. Returns the total number of bytes from p that were written.
func (sw *Writer) Write(p []byte) (int, error) {
    if sw.closed {
        return 0, ErrClosed
    }
    if sw.pwriter == nil {
        sw.next()
    }
    total := len(p)
    for len(p) > 0 {
        if sw.idx+len(p) > sw.chunkSize {
            n, err := sw.pwriter.Write(p[:sw.chunkSize-sw.idx])
            if err != nil {
                return total, sw.setErr(err)
            }
            if err := sw.flush(true); err != nil {
                return total, err
            }
            p = p[n:]
        } else {
            n, err := sw.pwriter.Write(p)
            if err != nil {
                return total, sw.setErr(err)
            }
            sw.idx += n
            p = p[n:]
        }
    }
    return total, nil
}

// Close implements the Close method of io.Closer. It closes the Writer.
// If the Writer is already closed, Close returns ErrAlreadyClosed.
func (sw *Writer) Close() error {
    if sw.closed {
        return ErrAlreadyClosed
    }
    sw.closed = true
    return sw.flush(false)
}

// Res returns the chunks written by the Writer so far.
func (sw *Writer) Res() []Chunk {
    return sw.chunks
}

// flush closes the current chunk, waits for it to be written to storage,
// and starts a new chunk if next is true.
func (sw *Writer) flush(next bool) error {
    if err := sw.pwriter.Close(); err != nil {
        return err
    }
    select {
    case err := <-sw.errCh:
        return sw.setErr(err)
    case chunk := <-sw.chunkCh:
        sw.chunks = append(sw.chunks, chunk)
    }
    if next {
        sw.next()
    }
    return nil
}

// next starts a new chunk for writing.
func (sw *Writer) next() {
    if !sw.closed {
        reader, writer := io.Pipe()
        sw.pwriter = writer
        go func() {
            url, size, err := sw.archive.WriteAttachment(reader)
            if err != nil {
                sw.errCh <- err
            } else {
                sw.idx = 0
                sw.chunkCh <- Chunk{URL: url, Size: size}
            }
        }()
    }
}

// setErr sets the last occurred error during stream write.
func (sw *Writer) setErr(err error) error {
    sw.streamErr = err
    return err
}
