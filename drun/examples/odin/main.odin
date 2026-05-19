package main

import "core:fmt"
import "core:runtime"

main :: proc() {
    fmt.println("Hello from Odin!")
    fmt.printf("Odin version: %s\n", ODIN_VERSION)
    fmt.printf("OS: %v\n", ODIN_OS)
    fmt.printf("Arch: %v\n", ODIN_ARCH)

    // Demonstrate some Odin features
    numbers := []int{1, 2, 3, 4, 5}
    sum := 0
    for n in numbers {
        sum += n
    }
    fmt.printf("Sum of %v = %d\n", numbers, sum)
}
