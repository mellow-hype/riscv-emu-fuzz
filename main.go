// A RISC-V 64 emulator in Go (lmao)
package main

import (
	"fmt"
	"io/ioutil"
)

const DEBUG_CONFIRM_RESET bool = true

// A struct that represents the emulated system
type Emulator struct {
	// Memory space of the emulator
	memory Mmu
}

// ELF Section
type Section struct {
	file_offset uint
	virt_addr   VirtAddr
	file_size   uint
	mem_size    uint
	permissions Perm
}

// Create a new Emulator instance
func NewEmulator(size uint) Emulator {
	// Create a new Emulator with size `size` of memory
	m := NewMmu(size)
	e := Emulator{memory: *m}
	return e
}

// Create a fork of the emulator
func (e *Emulator) fork() Emulator {
	m := e.memory.fork()
	forked := Emulator{memory: *m}
	return forked
}

// Load an executable using it's sections as described
func (e *Emulator) load(filePath string, sections []Section) {
	// Read the entire file directly into a slice
	file_contents, err := ioutil.ReadFile(filePath)
	if err != nil {
		panic("unable to read target file")
	}

	// Load each section
	for _, section := range sections {
		// set bytes to writable for the total mem_size (size of section in memory)
		e.memory.set_permission(section.virt_addr, section.mem_size, Perm{PERM_WRITE})

		// write in the file contents
		// file_offset = offset where section starts in file
		// file_size = size of the section data in the file
		// mem_size = total size of section in memory (can be greater than file_sz for uninit data)
		section_data := file_contents[section.file_offset : section.file_offset+section.file_size]
		e.memory.write_from(section.virt_addr, section_data)

		// handle padding (diff between mem_size and file_size is space for uninit mem, should be 0s)
		if section.mem_size > section.file_size {
			// padding bytes needed = mem_size - file_size
			padding := make([]uint8, section.mem_size-section.file_size)
			e.memory.write_from(
				// section virt_addr + section.file_size is the address at the end of the data we wrote
				VirtAddr{section.virt_addr.addr + section.file_size},
				// starting from that offset, we pad up to what would be the final total mem_size
				padding)
		}

		// Demote permissions back to what the section specifies
		e.memory.set_permission(section.virt_addr, section.mem_size, section.permissions)
	}
}

// Alloc, write, read
func (emu *Emulator) alloc_write_read(size uint) {
	// Allocate a `size` byte buffer from the guest addr space
	guest_alloc := emu.memory.allocate(size)

	// Write from buf_b to the space we allocated in guest_alloc_b
	buf := []uint8{}
	for i := uint(0); i < size; i++ {
		buf = append(buf, 0x66)
	}
	emu.memory.write_from(guest_alloc, buf)

	// Read the values from allocation to out_buf
	out_buf := make([]byte, size)
	emu.memory.read_into(guest_alloc, out_buf)
}

// Main
func main() {
	// Create the parent emulator with a 1024 * 1024 guest addr space.
	// This will be the clean state we use to reset forked emulator instances.
	emu := NewEmulator(1024 * 1024)

	// Load an executable into the emulator's address space
	emu.load("./r64i_test_app", []Section{
		// THESE VALUES WERE TAKEN DIRECTLY FROM THE OUTPUT OF `readelf -l`
		{
			file_offset: 0x0000000000000000,
			virt_addr:   VirtAddr{0x0000000000010000},
			file_size:   uint(0x0000000000000190),
			mem_size:    uint(0x0000000000000190),
			permissions: Perm{PERM_READ},
		},
		// THESE VALUES WERE TAKEN DIRECTLY FROM THE OUTPUT OF `readelf -l`
		{
			file_offset: 0x0000000000000190,
			virt_addr:   VirtAddr{0x0000000000011190},
			file_size:   uint(0x0000000000002598),
			mem_size:    uint(0x0000000000002598),
			// is READ|EXEC in the actual file, but we'll set it to EXEC only since it technically doesn't need
			// to be readable by the program itself (just by us)
			permissions: Perm{PERM_EXEC},
		},
		// THESE VALUES WERE TAKEN DIRECTLY FROM THE OUTPUT OF `readelf -l`
		{
			file_offset: 0x0000000000002728,
			virt_addr:   VirtAddr{0x0000000000014728},
			file_size:   uint(0x00000000000000f8),
			mem_size:    uint(0x0000000000000750),
			permissions: Perm{PERM_READ | PERM_WRITE},
		},
	})
	fmt.Println("loaded target binary into memory")

	{
		// Fork the emulator
		forked := emu.fork()

		// Read the first 4 bytes at the entry point of the executable to confirm we successfully loaded
		// the ELF sections.
		out_buf := make([]byte, 4)
		forked.memory.read_into_perms(
			// this is the entry point, taken from readelf output (also virt_addr of second section)
			VirtAddr{addr: uint(0x11190)},
			out_buf,
			Perm{PERM_EXEC}) // this region should be EXEC

		fmt.Printf("first 4 bytes at entry point (0x11190): ")
		for _, v := range out_buf {
			fmt.Printf("%#.2x ", v)
		}
		fmt.Println()
		// forked.memory.reset(&emu.memory)
	}

}
