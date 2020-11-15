// A RISC-V 64 emulator in Go (lmao)
package main

import (
	"fmt"
	"runtime"
)

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
	fmt.Printf("[%s]: guest MMU size: %#x\n", caller, len(emu.memory.memory))
	fmt.Printf("[%s]: guest MMU base address: %p\n", caller, mmu_mem_base)
	fmt.Printf("[%s]: guest VM allocations begin at: %p (vma:%#x)\n", caller, vm_mem_alloc, emu.memory.cur_alc.addr)
	fmt.Printf("===============================================================\n")

	// Allocate some memory from the emulator MMU
	fmt.Println("\n===== ORIGINAL EMULATOR =======")
	orig_alloc := emu.memory.allocate(1024)
	emu.memory.dirty_status()

	// Fork the emulator
	{
		forked := emu.fork()
		inbuf_2 := []uint8{66, 66, 66, 66}
		out_buf := make([]uint8, 4)

		// Write from inbuf_2 to the same allocated region but from the forked emulator
		forked.memory.write_from(orig_alloc, inbuf_2, uint(4))

		// Read that data back out
		forked.memory.read_into(orig_alloc, out_buf, uint(4))
		forked.memory.dirty_status()

		// Reset the forked emulator state back to the original
		forked.memory.reset(&emu.memory)

		// Read data back from the forked emulator to ensure we've returned back to the state before we forked
		// This should contain the values we wrote to the allocation before forking (`in_buf`)
		forked.memory.read_into(orig_alloc, out_buf, uint(4))
		forked.memory.dirty_status()
	}
}
