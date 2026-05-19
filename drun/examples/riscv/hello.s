# RISC-V Assembly - Hello World
# Assembles with: riscv64-linux-gnu-as -o hello.o hello.s
# Links with: riscv64-linux-gnu-ld -o hello hello.o
# Runs with: qemu-riscv64 ./hello

    .section .data
msg:
    .ascii "Hello from RISC-V Assembly!\n"
msg_len = . - msg

arch_msg:
    .ascii "Architecture: riscv64\n"
arch_len = . - arch_msg

    .section .text
    .globl _start

_start:
    # Write "Hello from RISC-V Assembly!"
    li      a7, 64          # syscall: write
    li      a0, 1           # fd: stdout
    la      a1, msg         # buf: message
    li      a2, msg_len     # count: length
    ecall

    # Write "Architecture: riscv64"
    li      a7, 64          # syscall: write
    li      a0, 1           # fd: stdout
    la      a1, arch_msg    # buf: message
    li      a2, arch_len    # count: length
    ecall

    # Exit with code 0
    li      a7, 93          # syscall: exit
    li      a0, 0           # status: 0
    ecall
