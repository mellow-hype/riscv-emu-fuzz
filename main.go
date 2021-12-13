// A RISC-V 64 emulator in Go (lmao)
package main

import (
	"fmt"
	"runtime"
)

const DEBUG_CONFIRM_RESET bool = true

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

// Return the calling function's name
func currentFunc() string {
	pc := make([]uintptr, 15)
	n := runtime.Callers(2, pc)
	frames := runtime.CallersFrames(pc[:n])
	frame, _ := frames.Next()
	return frame.Function
}

// Main entrypoint
func main() {
	// save the current function identifier
	caller := currentFunc()

	// Create the base Emulator with a 1024 * 1024 guest addr space
	// This will be the clean state we use to reset forked emulator instances
	emu := newEmu(1024 * 1024)
	mmu_mem_base := &emu.memory.memory[0]
	vm_mem_alloc := &emu.memory.memory[emu.memory.cur_alc.addr]

	// Allocate some memory from the emulator MMU
	PrintCl(Red, "\n===== PARENT EMULATOR =======")
	// SetColor(Red)
	fmt.Printf("[%s]: guest MMU size: %#x\n", caller, len(emu.memory.memory))
	fmt.Printf("[%s]: guest MMU base address: %p\n", caller, mmu_mem_base)
	fmt.Printf("[%s]: guest VM allocations begin at: %p (vma:%#x)\n", caller, vm_mem_alloc, emu.memory.cur_alc.addr)
	orig_alloc := emu.memory.allocate(4096)
	// in_buf := []uint8{77, 88, 99, 00}
	// emu.memory.write_from(orig_alloc, in_buf, uint(4))
	emu.memory.dirty_status()

	// Fork the emulator
	{
		// ResetColor()
		PrintCl(Cyan, "\n===== FORKED EMULATOR =======")
		forked := emu.fork()
		// SetColor(Cyan)

		fmt.Printf("[%s]: guest MMU size: %#x\n", caller, len(forked.memory.memory))
		fmt.Printf("[%s]: guest MMU base address: %p\n", caller, mmu_mem_base)
		fmt.Printf("[%s]: guest VM allocations begin at: %p (vma:%#x)\n", caller, vm_mem_alloc, forked.memory.cur_alc.addr)

		// creata a buf with some data to write to the allocated mem
		inbuf_2 := make([]uint8, 512)
		for x := uint(0); x < uint(8); x++ {
			inbuf_2[x] = 0x41
			inbuf_2[x*2] = 0x69
		}
		// a buf to read data back out to
		out_buf := make([]uint8, 32)

		// Write from inbuf_2 to the same allocated region but from the forked emulator
		// This will set the READ perm on the bytes that were written to
		PrintDbg("Attempt to write data to the region allocated by the original emulator")
		forked.memory.write_from(orig_alloc, inbuf_2, uint(len(inbuf_2)))

		// Read that data back out
		PrintDbg("Confirming data was successfully written")
		forked.memory.read_into(orig_alloc, out_buf, uint(8))
		PrintDbg("The newly written data should result in dirty blocks being updated")
		forked.memory.dirty_status()

		// Reset the forked emulator's state back to the original state it started with (from emu)
		PrintDbg("Resetting the forked MMU back to the original state")
		forked.memory.reset(&emu.memory)

	}

	PrintDbg("Operations complete, exiting")

}
