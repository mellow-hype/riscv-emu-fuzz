// A RISC-V 64 emulator in Go (lmao)
package main

import (
	"fmt"
	"runtime"
)

// Constants for permission bits
const PERM_READ uint8 = 1 << 0
const PERM_WRITE uint8 = 1 << 1
const PERM_EXEC uint8 = 1 << 2
const PERM_RAW uint8 = 1 << 3

// Block size used for resetting and tracking memory which has been modified
// A larger block size means fewer, but more expensive calls to memset, and the inverse
// if it's small.
// Sweet spot is 128-4096 bytes
const DIRTY_BLOCK_SIZE uint = 4096

// A permission byte which corresponds to a memory byte in the guest
// address space and defines the permissions it has
type Perm struct {
	uint8
}

// Holds a guest virtual address
type VirtAddr struct {
	addr uint
}

// Defines the structure of the MMU for a given emulator instance.
// This is an isolated memory space to be used by the emulator to load files
// and provide memory allocations to the underlying program the emulator is
// running.
type Mmu struct {
	// Block of memory which belongs to this guest. Offset 0 corresponds with
	// address 0x0 in the guest address space
	memory []uint8

	// Holds the permission bytes for each corresponding byte in memory
	permissions []Perm

	// Tracks blocks of memory in the MMU which are dirty and will need to be reset
	dirty []VirtAddr

	// Tracks which parts of memory have been dirtied
	dirty_bitmap []uint

	// Current base address of the next allocation
	cur_alc VirtAddr
}

// Create a new instance of the MMU struct with of size `size`
func newMmu(size uint) *Mmu {
	m := Mmu{
		memory:       make([]uint8, size),
		permissions:  make([]Perm, size),
		dirty:        make([]VirtAddr, 0, (size/DIRTY_BLOCK_SIZE)+1),
		dirty_bitmap: make([]uint, ((size/DIRTY_BLOCK_SIZE)/64)+1),
		cur_alc:      VirtAddr{addr: 0x10000},
	}
	return &m
}

// Mmu: Fork an existing MMU instance, copying over the parent MMU's memory
// and permissions.
func (m *Mmu) fork() *Mmu {
	fmt.Println("\n===== FORKING =======")
	size := uint(len(m.memory))
	clone := Mmu{
		memory:       make([]uint8, size),
		permissions:  make([]Perm, size),
		dirty:        make([]VirtAddr, 0, (size/DIRTY_BLOCK_SIZE)+1),
		dirty_bitmap: make([]uint, ((size/DIRTY_BLOCK_SIZE)/64)+1),
		cur_alc:      VirtAddr{addr: m.cur_alc.addr},
	}

	// Copy the parent MMU's current memory and permissions to the clone
	copy(clone.memory, m.memory)
	copy(clone.permissions, m.permissions)
	return &clone
}

// Mmm: Set permission `perm` for `size` bytes starting at `addr`
func (m *Mmu) set_permission(addr VirtAddr, size uint, perm Perm) {
	// Check if the permission change would go OOB
	if addr.addr+size > uint(len(m.memory)) {
		panic("Request would set permissions OOB of guest address space")
	}

	// Apply permission `perm` to `size` bytes starting at `addr`
	for i := addr.addr; i < addr.addr+size; i++ {
		m.permissions[i] = perm
	}
}

// Mmu: Restore memory to the state provided in `orig_mmu` (clears dirty blocks)
func (m *Mmu) reset(orig_mmu *Mmu) {
	fmt.Println("\n===== RESETTING FORK =======")
	for _, block := range m.dirty {
		// Get the start and end (virtual) addresses of the dirtied blocks of memory
		start := block.addr
		end := block.addr + DIRTY_BLOCK_SIZE

		// Zero the bitmap. `block.addr` was previously multiplied back up by DIRTY_BLOCK_SIZE, so we divide
		// back down for the bitmap indexing
		bm_idx := (block.addr / DIRTY_BLOCK_SIZE) / 64
		m.dirty_bitmap[bm_idx] = 0

		// Restore memory state and permissions from the state of the `orig_mmu`
		for idx := start; idx <= end; idx++ {
			m.memory[idx] = orig_mmu.memory[idx]
			m.permissions[idx] = orig_mmu.permissions[idx]
		}
	}

	// Clear the dirty block list
	// NOTE: KEEPS THE ALLOCATED MEMORY, INDEXING BACK INTO THE LIST WILL FIND THESE VALUES
	m.dirty = m.dirty[:0]
}

// Mmu: allocate a region of memory as RW in the guest address space
func (m *Mmu) allocate(size uint) VirtAddr {
	// 16-byte align the allocation size
	align_size := (size + 0xf) &^ 0xf

	// Get the current allocation base addr
	base := m.cur_alc

	// Check if the last allocation went beyond the guest address space
	if base.addr+align_size >= uint(len(m.memory)) {
		panic("allocation would go beyond the guest address space")
	}

	// Update the cur_alc, adding the size of the new allocation
	m.cur_alc.addr = m.cur_alc.addr + align_size
	fmt.Printf(
		"[%s]: allocated %d bytes in guest addr space at: vma:%#x (phy:%p)\n", currentFunc(), size, base.addr, &m.memory[base.addr],
	)

	// Mark newly allocated memory as uninitialized and writable
	fmt.Printf(
		"[%s]: setting PERM_RAW|PERM_WRITE for %d bytes at: vma:%#x (phy:%p)\n", currentFunc(), size, base.addr, &m.memory[base.addr],
	)
	m.set_permission(base, size, Perm{PERM_RAW | PERM_WRITE})
	return base

}

// Mmu: Write bytes from `buf` to `addr`
func (m *Mmu) write_from(addr VirtAddr, buf []uint8, size uint) {
	// Check if the write operation would go OOB
	if addr.addr+size > uint(len(m.memory)) {
		panic("Operation would write OOB of guest address space")
	}

	// Check if the read operation would go OOB of the current allocation
	if addr.addr+size > uint(m.cur_alc.addr) {
		panic("Operation would write beyond it's allocation")
	}

	// Check if the read operation would go OOB of buf
	if size > uint(len(buf)) {
		panic("bytes to write from buffer is greater than size of buffer")
	}

	// Check permissions
	has_raw := 0
	for _, v := range m.permissions[addr.addr : addr.addr+size] {
		// check for RAW perm on each byte
		if (v.uint8 & PERM_RAW) != 0 {
			has_raw |= 1
		}
		// check for write perm bit on each byte
		if (v.uint8 & PERM_WRITE) == 0 {
			panic("Write permission denied")
		}
	}

	// Write bytes from `buf` to `addr`
	fmt.Printf(
		"[%s]: writing %d bytes to vma:%#x (phy:%p)\n", currentFunc(), len(buf), addr.addr, &m.memory[addr.addr],
	)
	for i := uint(0); i < size; i++ {
		m.memory[addr.addr+i] = buf[i]
	}
	fmt.Printf("[%s]: wrote: %v\n", currentFunc(), buf[:size])

	// Compute the blocks for dirtied bits. We divide the start address and end address by the
	// dirty block size to break them down into blocks.
	var block_start uint = (addr.addr / DIRTY_BLOCK_SIZE)
	var block_end uint = (addr.addr + size) / DIRTY_BLOCK_SIZE
	var block_size uint = block_end - block_start
	if block_size == 0 {
		block_size += 1
	}
	fmt.Printf("[%s]: block_start = %d | block_end = %d | block_size = %d\n", currentFunc(), block_start, block_end, block_size)

	// Update dirty list and the bitmap with each block found
	for i := block_start; i <= block_end; i++ {
		// Determine the bitmap position of the dirty block
		idx := i / 64
		bit := i % 64

		// If the value at dirty_bitmap[idx] is 0, this hasn't been marked as dirty yet
		if m.dirty_bitmap[idx]&(1<<bit) == 0 {
			// Add it to the dirty list
			m.dirty = append(m.dirty, VirtAddr{addr: i * DIRTY_BLOCK_SIZE})

			// Update the dirty bitmap for this block
			m.dirty_bitmap[idx] |= 1 << bit
			fmt.Printf("[%s]: added block to dirty list and updated bitmap\n", currentFunc())
		}
	}

	// Update RaW bits
	if has_raw == 1 {
		for i := uint(0); i < size; i++ {
			if (m.permissions[addr.addr+i].uint8 & PERM_RAW) != 0 {
				// Mark memory as readable now that it's been written to
				m.permissions[addr.addr+i] = Perm{m.permissions[addr.addr+i].uint8 | PERM_READ}
			}
		}
	}

}

// Mmu: Read bytes from `addr` into `buf`
func (m *Mmu) read_into(addr VirtAddr, buf []uint8, size uint) {
	// Check if the read operation would go OOB
	if addr.addr+size > uint(len(m.memory)) {
		panic("Operation would read OOB of guest address space")
	}

	// Check if the read operation would go OOB of the current allocation
	if addr.addr+size > uint(m.cur_alc.addr) {
		panic("Operation would read beyond the currently allocated space")
	}

	// Check if the read operation would go OOB of the out_buf
	if size > uint(len(buf)) {
		panic("bytes to read from addr is greater than size of dst buffer")
	}

	// Check permissions
	for _, v := range m.permissions[addr.addr : addr.addr+size] {
		// check for read perm bit on each byte, return error if any don't have it set
		if !((v.uint8 & PERM_READ) != 0) {
			panic("Read permission denied")
		}
	}

	// Read bytes from `addr` to `buf`
	fmt.Printf("[%s]: reading %d bytes from vma:%#x (phy:%p)\n", currentFunc(), len(buf), addr.addr, &m.memory[addr.addr])
	for i := uint(0); i < size; i++ {
		buf[i] = m.memory[addr.addr+i]
	}
	fmt.Printf("[%s]: read %v\n", currentFunc(), buf)
}

// Print the status of the dirty list and dirty_bitmap
func (m *Mmu) dirty_status() {
	caller := currentFunc()
	fmt.Printf("[%s]: dirty %v\n", caller, m.dirty)
	for i, v := range m.dirty {
		fmt.Printf("[%s]: dirty[%d] == %#x\n", caller, i, v.addr)
	}

	fmt.Printf("[%s]: dirty_bitmap length: %d\n", caller, len(m.dirty_bitmap))
	for i, v := range m.dirty_bitmap {
		fmt.Printf("[%s]: dirty_bitmap[%d] == %#x\n", caller, i, v)
	}
}


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
