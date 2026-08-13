package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

type Generator struct {
	counter uint64
	nodeID  string
}

func NewGenerator() *Generator {
	nodeID := generateNodeID()
	return &Generator{
		counter: 0,
		nodeID:  nodeID,
	}
}

func generateNodeID() string {
	b := make([]byte, 6)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		return "000000000000"
	}
	return hex.EncodeToString(b)
}

func (g *Generator) Generate() string {
	count := atomic.AddUint64(&g.counter, 1)
	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("%s%016x%012x", g.nodeID, timestamp, count)
}

func (g *Generator) GenerateUUID() string {
	b := make([]byte, 16)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		panic(fmt.Sprintf("failed to generate uuid: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4],
		b[4:6],
		b[6:8],
		b[8:10],
		b[10:16])
}

func (g *Generator) GenerateShortID() string {
	b := make([]byte, 8)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		panic(fmt.Sprintf("failed to generate short id: %v", err))
	}
	return hex.EncodeToString(b)
}

func ValidateUUID(id string) bool {
	if len(id) != 36 {
		return false
	}
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func MustUUID(id string) string {
	if ValidateUUID(id) {
		return id
	}
	return ""
}

type IDGenerator interface {
	Generate() string
	GenerateUUID() string
	GenerateShortID() string
}

var _ IDGenerator = (*Generator)(nil)

func NewWithPrefix(prefix string) *PrefixedGenerator {
	return &PrefixedGenerator{
		prefix:    prefix,
		generator: NewGenerator(),
	}
}

type PrefixedGenerator struct {
	prefix    string
	generator *Generator
}

func (pg *PrefixedGenerator) Generate() string {
	return pg.prefix + "_" + pg.generator.GenerateUUID()
}

func (pg *PrefixedGenerator) GenerateUUID() string {
	return pg.generator.GenerateUUID()
}

func (pg *PrefixedGenerator) GenerateShortID() string {
	return pg.prefix + "_" + pg.generator.GenerateShortID()
}

type SequentialGenerator struct {
	counter uint64
	prefix  string
}

func NewSequentialGenerator(prefix string) *SequentialGenerator {
	return &SequentialGenerator{
		counter: 0,
		prefix:  prefix,
	}
}

func (sg *SequentialGenerator) Generate() string {
	count := atomic.AddUint64(&sg.counter, 1)
	return fmt.Sprintf("%s%016d", sg.prefix, count)
}

func (sg *SequentialGenerator) GenerateUUID() string {
	return sg.Generate()
}

func (sg *SequentialGenerator) GenerateShortID() string {
	count := atomic.AddUint64(&sg.counter, 1)
	return fmt.Sprintf("%s%04d", sg.prefix, count)
}

type TimeBasedGenerator struct {
	counter uint64
}

func NewTimeBasedGenerator() *TimeBasedGenerator {
	return &TimeBasedGenerator{}
}

func (tg *TimeBasedGenerator) Generate() string {
	count := atomic.AddUint64(&tg.counter, 1)
	now := time.Now()
	return fmt.Sprintf("%d%05d", now.UnixNano(), count)
}

func (tg *TimeBasedGenerator) GenerateUUID() string {
	b := make([]byte, 16)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (tg *TimeBasedGenerator) GenerateShortID() string {
	b := make([]byte, 6)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func IsValidID(id string) bool {
	if id == "" {
		return false
	}
	if len(id) < 8 {
		return false
	}
	return true
}