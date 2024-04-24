package dca

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

var (
	ErrVoiceConnClosed = errors.New("Voice connection closed")
)

// StreamingSession provides an easy way to directly transmit opus audio
// to discord from an encode session.
type StreamingSession struct {
	sync.Mutex

	// If this channel is not nil, an error will be sen when finished (or nil if no error)
	done chan error

	source OpusReader
	vc     *discordgo.VoiceConnection

	repeat     bool
	end        bool //lol, i hate this but let it be
	paused     bool
	framesSent int

	finished bool
	running  bool
	err      error // If an error occured and we had to stop
}

// Creates a new stream from an Opusreader.
// source   : The source of the opus frames to be sent, either from an encoder or decoder.
// vc       : The voice connecion to stream to.
// done     : If not nil, an error will be sent on it when completed.
func NewStream(source OpusReader, vc *discordgo.VoiceConnection, done chan error) *StreamingSession {
	session := &StreamingSession{
		source: source,
		vc:     vc,
		done:   done,
	}

	go session.stream()

	return session
}

func (s *StreamingSession) stream() {
	// Check if we are already running and if so stop
	s.Lock()
	if s.running {
		s.Unlock()
		panic("Stream is already running!")
		return
	}
	s.running = true
	s.Unlock()

	defer func() {
		s.Lock()
		s.running = false
		s.Unlock()
	}()

	for {
		s.Lock()

		if s.paused {
			s.Unlock()
			return
		}

		if s.end {
			s.end = false
			s.running = false
			s.repeat = false
			s.Unlock()
			return
		}

		s.Unlock()
		err := s.readNext()
		if err != nil {
			s.Lock()

			s.finished = true
			if err != io.EOF {
				s.err = err
			}

			if s.done != nil {
				go func() {
					s.done <- err
				}()
			}

			s.Unlock()
			if !s.repeat {
				s.repeat = false
				break
			} else {
				s.framesSent = 0
				s.SetPlaybackPosition(0)
			}
		}
	}
}

func (s *StreamingSession) readNext() error {
	opus, err := s.source.OpusFrame()
	if err != nil {
		return err
	}

	// Timeout after 100ms (Maybe this needs to be changed?)
	timeOut := time.NewTimer(time.Second)

	// This will attempt to send on the channel before the timeout, which is 1s
	select {
	case <-timeOut.C:
		return ErrVoiceConnClosed
	case s.vc.OpusSend <- opus:
		timeOut.Stop()
	}

	s.Lock()
	s.framesSent++
	s.Unlock()

	return nil
}

// Paused returns wether the sream is paused or not
func (s *StreamingSession) Paused() bool {
	s.Lock()
	p := s.paused
	s.Unlock()

	return p
}

// SetPaused provides pause/unpause functionality
func (s *StreamingSession) SetPaused(paused bool) {
	s.Lock()

	if s.finished {
		s.Unlock()
		return
	}

	// Already running
	if !paused && s.running {
		if s.paused {
			// Was set to stop running after next frame so undo this
			s.paused = false
		}

		s.Unlock()
		return
	}

	// Already stopped
	if paused && !s.running {
		// Not running, but starting up..
		if !s.paused {
			s.paused = true
		}

		s.Unlock()
		return
	}

	// Time to start it up again
	if !s.running && s.paused && !paused {
		go s.stream()
	}

	s.paused = paused
	s.Unlock()
}

// PlaybackPosition returns the the duration of content we have transmitted so far
func (s *StreamingSession) PlaybackPosition() time.Duration {
	s.Lock()
	dur := time.Duration(s.framesSent) * s.source.FrameDuration()
	s.Unlock()
	return dur
}

func (s *StreamingSession) SetPlaybackPosition(time time.Duration) {
	s.Lock()
	frame := int(time * s.source.FrameDuration())
	s.framesSent = frame
	s.Unlock()
}

// Finished returns wether the stream finished or not, and any error that caused it to stop
func (s *StreamingSession) Finished() (bool, error) {
	s.Lock()
	err := s.err
	fin := s.finished
	s.Unlock()

	return fin, err
}

// Stop current stream from playing more music
func (s *StreamingSession) SetFinished() {
	s.Lock()

	if s.finished {
		s.Unlock()
		return
	}

	s.end = true

	s.Unlock()
}

// checks if the stream is repeating
func (s *StreamingSession) Repeat() bool {
	s.Lock()

	if s.finished || !s.running {
		s.Unlock()
		return false
	}

	repeat := s.repeat

	s.Unlock()
	return repeat
}

func (s *StreamingSession) SetRepeat(repeat bool) {
	s.Lock()

	if s.finished || !s.running {
		s.Unlock()
		return
	}

	s.repeat = repeat

	s.Unlock()
}
