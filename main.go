package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"go.bug.st/serial"
)

type Radio struct {
	Port     serial.Port
	PortPath string
	BaudRate int
	PortRW   *bufio.ReadWriter
	Model    string
	Memory   []MemoryEntry
}

func (r *Radio) Connect() error {
	var err error
	r.Port, err = serial.Open(r.PortPath, &serial.Mode{
		BaudRate: r.BaudRate,
	})
	if err != nil {
		return fmt.Errorf("error opening serial port: %w", err)
	}
	r.PortRW = bufio.NewReadWriter(
		bufio.NewReader(r.Port),
		bufio.NewWriter(r.Port),
	)
	return nil
}

func (r *Radio) WriteString(command string) error {
	_, err := r.PortRW.WriteString(command)
	if err != nil {
		return fmt.Errorf("error writing string %s to radio: %w", command, err)
	}
	err = r.PortRW.Flush()
	log.Debug().Str("send", command).Msg("serial")
	if err != nil {
		return fmt.Errorf("error flushing serial IO while writing string %s to radio: %w", command, err)
	}
	return nil
}

func (r *Radio) ReadString() (string, error) {
	str, err := r.PortRW.ReadString('\r')
	if err != nil {
		return "", fmt.Errorf("error reading from radio: %w", err)
	}
	log.Debug().Str("recv", str).Msg("serial")
	return str, nil
}

func (r *Radio) WriteReadString(command string) (string, error) {
	err := r.WriteString(command)
	if err != nil {
		return "", fmt.Errorf("error writing to radio: %w", err)
	}
	line, err := r.ReadString()
	if err != nil {
		return "", fmt.Errorf("error reading from radio: %w", err)
	}
	if strings.HasPrefix(line, "?") {
		return "", fmt.Errorf("error writing \"%s\" to radio: radio sent ? and did not understood us", command)
	}
	return line, nil
}

func (r *Radio) Identify() error {
	line, err := r.WriteReadString(IDCommandFormat)
	if err != nil {
		return fmt.Errorf("error while reading ident sequence from radio: %w", err)
	}
	_, err = fmt.Sscanf(line, IDFormat, &r.Model)
	if err != nil {
		return fmt.Errorf("error while parsing identification sequence from radio: %w", err)
	}
	return nil
}

func (r *Radio) ReadChannel(channel int) (m MemoryEntry, e error) {
	chline, err := r.WriteReadString(
		fmt.Sprintf(MECommandFormat, channel),
	)
	if err != nil {
		return MemoryEntry{}, fmt.Errorf("error while reading channel: %w", err)
	}
	nameline, err := r.WriteReadString(
		fmt.Sprintf(MNCommandFormat, channel),
	)
	if err != nil {
		return MemoryEntry{}, fmt.Errorf("error while reading channel name: %w", err)
	}
	err = m.ReadChannelLine(chline)
	if err != nil {
		return MemoryEntry{}, fmt.Errorf("error parsing channel line: %s", err)
	}
	err = m.ReadNameLine(nameline)
	if err != nil {
		return MemoryEntry{}, fmt.Errorf("error parsing name line: %s", err)
	}
	return m, nil
}

func (r *Radio) WriteChannel(channel int) error {
	ch := func() MemoryEntry {
		for _, m := range r.Memory {
			if m.Number == uint16(channel) {
				return m
			}
		}
		return MemoryEntry{}
	}()
	if ch.RXFrequency == 0 {
		return fmt.Errorf("error: attempted to write empty channel %d", channel)
	}

	_, err := r.WriteReadString(
		fmt.Sprintf("ME %03d,C\r", channel),
	)
	if err != nil {
		return fmt.Errorf("error clearing channel %d before write: %w", channel, err)
	}
	chline := ch.WriteChannelLine() + "\r"
	_, err = r.WriteReadString(chline)
	if err != nil {
		return fmt.Errorf("error writing channel %d data to radio: %w", channel, err)
	}
	/*
		if !strings.HasPrefix(str, "ME") {
			return fmt.Errorf("error writing channel %d data to radio: did not got ME answer", channel)
		}
	*/

	nameline := ch.WriteNameLine() + "\r"
	_, err = r.WriteReadString(nameline)
	if err != nil {
		return fmt.Errorf("error writing channel %d name to radio: %w", channel, err)
	}
	/*
		if !strings.HasPrefix(str, "MN") {
			return fmt.Errorf("error writing channel %d data to radio: did not got MN answer", channel)
		}
	*/

	return nil
}

func (r *Radio) ReadMemory() error {
	var err error
	for i := 0; i <= 999; i++ {
		r.Memory[i], err = r.ReadChannel(i)
		if err != nil {
			return fmt.Errorf("error reading memory: %w", err)
		}
	}
	return nil
}

func (r *Radio) OccupedChannels() (v []MemoryEntry) {
	for _, m := range r.Memory {
		if m.RXFrequency != 0 {
			v = append(v, m)
		}
	}
	return v
}

func (r *Radio) WriteMemory() error {
	for _, m := range r.OccupedChannels() {
		err := r.WriteChannel(int(m.Number))
		if err != nil {
			return fmt.Errorf("error writing channel %d to radio: %w", m.Number, err)
		}
	}
	return nil
}

type MemoryEntry struct {
	Number          uint16 `json:",omitempty"`
	RXFrequency     uint32 `json:",omitempty"`
	RXStepSize      uint8  `json:",omitempty"`
	ShiftDirection  uint8  `json:",omitempty"`
	ReverseEnabled  uint8  `json:",omitempty"`
	ToneEnabled     uint8  `json:",omitempty"`
	CTCSSEnabled    uint8  `json:",omitempty"`
	DCSEnabled      uint8  `json:",omitempty"`
	ToneFrequency   uint16 `json:",omitempty"`
	CTCSSFrequency  uint16 `json:",omitempty"`
	DCSFrequency    uint16 `json:",omitempty"`
	OffsetFrequency uint32 `json:",omitempty"`
	Mode            uint8  `json:",omitempty"`
	TXFrequency     uint32 `json:",omitempty"`
	TXStepSize      uint8  `json:",omitempty"`
	LockOut         uint8  `json:",omitempty"`
	Name            string `json:",omitempty"`
}

const (
	MEFormat        = "ME %03d,%010d,%1d,%1d,%1d,%1d,%1d,%1d,%02d,%02d,%03d,%08d,%1d,%010d,%1d,%1d"
	MNFormat        = "MN %03d,%s"
	IDCommandFormat = "ID\r"
	MECommandFormat = "ME %03d\r"
	MNCommandFormat = "MN %03d\r"
	IDFormat        = "ID %s"
)

func (m *MemoryEntry) StructFieldPointers() []interface{} {
	val := reflect.ValueOf(m).Elem()
	v := make([]interface{}, val.NumField())
	for i := 0; i < val.NumField(); i++ {
		v[i] = val.FieldByIndex([]int{i}).Addr().Interface()
	}
	return v
}

func (m *MemoryEntry) StructFieldValues() []interface{} {
	val := reflect.ValueOf(m).Elem()
	v := make([]interface{}, val.NumField())
	for i := 0; i < val.NumField(); i++ {
		v[i] = val.FieldByIndex([]int{i}).Interface()
	}
	return v
}

func (m *MemoryEntry) ReadNameLine(line string) error {
	if line != "N\r" {
		line = strings.TrimSuffix(line, "\r")
		items := strings.Split(line, ",")
		if !strings.HasPrefix(items[0], "MN") {
			return fmt.Errorf("error reading nameline: \"%s\"", line)
		}
		if (len(items) < 2) || (items[1] == "") {
			m.Name = ""
			return nil
		}
		m.Name = items[1]
	}
	return nil
}

func (m *MemoryEntry) ReadChannelLine(line string) error {
	if line != "N\r" {
		items, err := fmt.Sscanf(line, MEFormat, m.StructFieldPointers()[:16]...)
		if err != nil {
			return fmt.Errorf("error parsing channel line: \"%s\"", line)
		}
		if items < 15 {
			return fmt.Errorf("error parsing channel line: ME command returned less items")
		}
	}
	return nil
}

func (m *MemoryEntry) WriteNameLine() (s string) {
	var n string
	if len(m.Name) > 8 {
		n = m.Name[:8]
	} else {
		n = m.Name
	}
	return fmt.Sprintf(MNFormat, m.Number, n)
}

func (m *MemoryEntry) WriteChannelLine() (s string) {
	return fmt.Sprintf(MEFormat, m.StructFieldValues()[:16]...)
}

func NewRadio(portpath string, baudrate int) (*Radio, error) {
	var err error
	r := &Radio{
		PortPath: portpath,
		BaudRate: baudrate,
		Memory:   make([]MemoryEntry, 1000),
	}
	err = r.Connect()
	if err != nil {
		return nil, fmt.Errorf("cannot create new radio: %w", err)
	}
	return r, nil
}

func main() {
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.Stamp})
	r, err := NewRadio("/dev/ttyUSB0", 9600)
	if err != nil {
		log.Fatal().Err(err).Msg("error opening radio")
	}
	r.Identify()
	log.Info().Str("radio model", r.Model).Msg("Connected")

	/*
		log.Info().Msg("Reading memory...")
		if err := r.ReadMemory(); err != nil {
			log.Fatal().Err(err).Msg("error reading memory")
		}
		log.Info().Msg("Reading done.")

		log.Info().Msg("Dumping memory to file...")
		j, err := json.MarshalIndent(r.OccupedChannels(), "", "  ")
		if err != nil {
			log.Fatal().Err(err).Msg("error marshalling memory")
		}
		err = os.WriteFile("./kenwood-memory.json", j, 0644)
		if err != nil {
			log.Fatal().Err(err).Msg("error writing memory to file")
		}
		log.Info().Msg("Dumping memory to file done")

		log.Info().Msg("Writing memory...")
		if err := r.WriteMemory(); err != nil {
			log.Fatal().Err(err).Msg("error writing memory")
		}
		log.Info().Msg("Writing memory done.")
	*/

	log.Info().Msg("Loading memory from file...")
	jj, err := os.ReadFile("./kenwood-memory.json")
	if err != nil {
		log.Fatal().Err(err).Msg("error reading memory dump")
	}

	var loadedMemories []MemoryEntry
	err = json.Unmarshal(jj, &loadedMemories)
	if err != nil {
		log.Fatal().Err(err).Msg("error parsing memory dump")
	}
	r.Memory = nil
	r.Memory = make([]MemoryEntry, 1000)
	copy(r.Memory, loadedMemories)
	log.Info().Msg("Memory loaded from file...")

	log.Info().Msg("Writing memory...")
	if err := r.WriteMemory(); err != nil {
		log.Fatal().Err(err).Msg("error writing memory")
	}
	log.Info().Msg("Writing memory done.")

}
