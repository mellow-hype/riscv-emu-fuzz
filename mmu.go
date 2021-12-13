// The memory management unit for the emulator
package main

import (
	"fmt"
)

// Constants for permission bits
const PERM_READ uint8 = 1 << 0
const PERM_WRITE uint8 = 1 << 1
const PERM_EXEC uint8 = 1 << 2
const PERM_RAW uint8 = 1 << 3

// Block size used for resetting and tracking memory which has been modified
// Sweet spot is 128-4096 bytes.
// i.e. every 256 bytes dirtied == 1 block
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
	dirty []uint

	// Tracks which parts of memory have been dirtied
	dirty_bitmap []uint

	// Current base address of the next allocation
	cur_alc VirtAddr
}

// Create a new instance of the MMU struct with of size `size`
func newMmu(size uint) *Mmu {
	m := Mmu{
		memory:      make([]uint8, size),
		permissions: make([]Perm, size),
		// size / DIRTY_BLOCK_SIZE breaks the total size into chunks
		dirty:        make([]uint, 0, (size/DIRTY_BLOCK_SIZE + 1)),
		dirty_bitmap: make([]uint, ((size/DIRTY_BLOCK_SIZE)/64 + 1)),
		cur_alc:      VirtAddr{addr: 0x10000},
	}
	return &m
}

// Mmu: Fork an existing MMU instance, copying over the parent MMU's memory
// and permissions.
func (m *Mmu) fork() *Mmu {
	size := uint(len(m.memory))
	clone := newMmu(size)
	// clone := Mmu{
	// 	memory:       make([]uint8, size),
	// 	permissions:  make([]Perm, size),
	// 	dirty:        make([]VirtAddr, 0, (size/DIRTY_BLOCK_SIZE + 1)), // +1 in case div results in 0
	// 	dirty_bitmap: make([]uint, ((size/DIRTY_BLOCK_SIZE)/64 + 1)),
	// 	cur_alc:      VirtAddr{addr: m.cur_alc.addr},
	// }

	// Copy the parent MMU's current memory and permissions to the clone
	copy(clone.memory, m.memory)
	copy(clone.permissions, m.permissions)
	clone.cur_alc.addr = m.cur_alc.addr
	return clone
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
	for _, block := range m.dirty {
		// Get the start and end (virtual) addresses of the dirtied blocks of memory
		// `block`` is multiplied up by BLOCK_SIZE to get the vma (was divided by block_size to calculate block)
		start := block * DIRTY_BLOCK_SIZE
		end := (block + 1) * DIRTY_BLOCK_SIZE

		// OPT: Zero the bitmap. This hits wide but its okay because we do 64-bit write
		// anyway, no reason to compute the bit index
		// start / BLOCK_SIZE / 64 because start == block.addr and we need to break the addr down to it's blocks
		m.dirty_bitmap[block/64] = 0

		//restore memory state and permissions back to original
		copy(m.memory[start:end], orig_mmu.memory[start:end])
		copy(m.permissions[start:end], orig_mmu.permissions[start:end])

		// Restore memory state and permissions from the state of the `orig_mmu`
		// for idx := start; idx <= end; idx++ {
		// 	m.memory[idx] = orig_mmu.memory[idx]
		// 	m.permissions[idx] = orig_mmu.permissions[idx]
		// }
		fmt.Printf(
			"[%s]: reset dirtied blocks at address range vma:%#x-%#x\n", currentFunc(), block*DIRTY_BLOCK_SIZE, end*DIRTY_BLOCK_SIZE,
		)
	}

	// NOTE: KEEPS THE ALLOCATED MEMORY, INDEXING BACK INTO THE LIST WILL FIND THESE VALUES
	// m.dirty = orig_mmu.dirty
	// Clear the dirty block list
	m.dirty = m.dirty[:0]
	m.dirty_status()
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
	// fmt.Printf("[%s]: payload written:\n %v\n", currentFunc(), buf[:size])

	// Compute the blocks for dirtied bits. We divide the start and end address by the
	// dirty block size to break them down into blocks.
	var block_start uint = addr.addr / DIRTY_BLOCK_SIZE
	var block_end uint = (addr.addr + uint(len(buf))) / DIRTY_BLOCK_SIZE
	var block_size uint = (block_end - block_start)
	if block_size == 0 {
		block_size += 1
	}

	// Update dirty list and the bitmap for each block found
	for block := block_start; block < block_end+1; block++ {
		// for i := uint(1); i < block_size+1; i++ {
		// Determine the bitmap position of the dirty block
		idx := block_start / 64
		bit := block_start % 64

		// If the value at dirty_bitmap[idx] is 0, this hasn't been marked as dirty yet
		if m.dirty_bitmap[idx]&(1<<bit) == 0 {
			// Add it to the dirty list
			m.dirty = append(m.dirty, block)

			// Update the dirty bitmap for this block
			m.dirty_bitmap[idx] |= 1 << bit
		}
	}

	fmt.Printf("[%s]: added %d block(s) to dirty list and updated bitmap\n", currentFunc(), block_size)
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
	fmt.Printf("[%s]: reading %d bytes from vma:%#x (phy:%p)\n", currentFunc(), size, addr.addr, &m.memory[addr.addr])
	for i := uint(0); i < size; i++ {
		buf[i] = m.memory[addr.addr+i]
	}
	// fmt.Printf("[%s]: data %v\n", currentFunc(), buf[:size])
}

// Print the status of the dirty list and dirty_bitmap
func (m *Mmu) dirty_status() {
	caller := currentFunc()
	// fmt.Printf("[%s]: dirty %v\n", caller, m.dirty)

	fmt.Printf("[%s]: dirty_bitmap:\n\t", caller)
	fmt.Printf("%s| ", White)
	for x, v := range m.dirty_bitmap {
		// highlight dirtied bits in red
		if v > 0 {
			print(Red)
		} else {
			print(Green)
		}
		// print the bit
		fmt.Printf("%#x", v)
		// reset color for delimiter
		print(White)
		fmt.Printf(" | ")

		// break every 8 bits
		if (x+1)%8 == 0 && x < len(m.dirty_bitmap)-1 {
			fmt.Printf("\n\t| ")
		}
		ResetColor()
	}
	fmt.Println("")
}
