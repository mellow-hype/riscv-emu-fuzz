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
func newEmu(size uint) Emulator {
	// Create a new Emulator with size `size` of memory
	m := newMmu(size)
	e := Emulator{memory: *m}
	return e
}

// Create a fork of the emulator
func (e *Emulator) fork() Emulator {
	m := e.memory.fork()
	forked := Emulator{memory: *m}
	return forked
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
		fmt.Printf("[%s]: dirty[%d] == %#x\n", caller, i, v)
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

	// Create the parent emulator with a 1024 * 1024 guest addr space.
	// This will be the clean state we use to reset forked emulator instances.
	PrintCl(Red, "\n===== PARENT EMULATOR =======")
	emu := newEmu(1024 * 1024)
	fmt.Printf("[%s]: MMU size: %#x\n", caller, len(emu.memory.memory))

	// Allocate some memory from the parent emulator MMU
	orig_alloc := emu.memory.allocate(4096)
	// Write data to the allocated region. This will set the READ perm on the bytes that were written to and update
	// the dirty blocks.
	indata := []byte("abcd")
	PrintDbg("writing %d bytes (%d) @ vma:%#x", len(indata), indata, orig_alloc.addr)
	emu.memory.write_from(orig_alloc, indata, uint(len(indata)))
	emu.memory.dirty_status()

	// Fork the emulator
	{
		PrintCl(Cyan, "\n===== FORKED EMULATOR =======")
		forked := emu.fork()

		// There should be no dirty blocks in the freshly forked emulator, regardless of the parent emulator's
		// state
		forked.memory.dirty_status()

		// Write data to the same allocated region but from the forked emulator.
		indata := []byte("AAAA")
		PrintDbg("writing %d bytes (%d) @ vma:%#x", len(indata), indata, orig_alloc.addr)
		forked.memory.write_from(orig_alloc, indata, 4)
		forked.memory.dirty_status()

		// Read the data back out
		out_buf := make([]uint8, 32)
		forked.memory.read_into(orig_alloc, out_buf, uint(4))
		fmt.Println("data before reset:", out_buf[:4])

		// // Reset the forked emulator's state back to the original state it started with (from emu)
		PrintDbg("Resetting the forked MMU back to the original state")
		forked.memory.reset(&emu.memory)
		forked.memory.dirty_status()

		// // Read that data back out to confirm state has been reset back to the original values set prior to fork.
		// // If no data had been written prior to forking, this should fail because READ perms have not been
		// // set on those bytes (Read-after-Write). This prevents uninitialized data from being read.
		forked.memory.read_into(orig_alloc, out_buf, uint(4))
		fmt.Println("data after reset:", out_buf[:4])
	}

}
