package main

import (
	"fmt"
)

// A struct that represents the emulated system
type Emulator struct {
	// Memory space of the emulator
	memory Mmu
}

// Create a new Emulator instance
func newEmu(size uint) *Emulator {
	// Create a new Emulator with size `size` of memory
	m := newMmu(size)
	e := Emulator{memory: *m}
	return &e
}

// Create a fork of the emulator
func (e *Emulator) fork() *Emulator {
	m := e.memory.fork()
	forked := Emulator{memory: *m}
	return &forked
}

// Alloc, write, read
func (emu *Emulator) alloc_write_read(size uint) {
	// save the current function identifier
	caller := currentFunc()

	// Allocate a `size` byte buffer from the guest addr space
	guest_alloc := emu.memory.allocate(size)

	// Write from buf_b to the space we allocated in guest_alloc_b
	buf := []uint8{}
	for i := uint(0); i < size; i++ {
		buf = append(buf, 0x66)
	}
	emu.memory.write_from(guest_alloc, buf, uint(len(buf)))

	// Read the values from allocation to out_buf
	out_buf := make([]uint8, size)
	emu.memory.read_into(guest_alloc, out_buf, uint(len(out_buf)))

	// Show dirtied blocks
	fmt.Printf("[%s]: dirty %v\n", caller, emu.memory.dirty)
	for i, v := range emu.memory.dirty {
		fmt.Printf("[%s]: dirty[%d] == %#x\n", caller, i, v.addr)
	}
	fmt.Printf("[%s]: dirty_bitmap length: %d\n", caller, len(emu.memory.dirty_bitmap))
	for i, v := range emu.memory.dirty_bitmap {
		fmt.Printf("[%s]: dirty_bitmap[%d] == %#x\n", caller, i, v)
	}

}
