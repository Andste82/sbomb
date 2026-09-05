# Bare-metal ARM. No newlib is assumed, so the fixtures link freestanding --
# which is also what firmware projects, the tool's primary target, do.
set(CMAKE_SYSTEM_NAME Generic)
set(CMAKE_SYSTEM_PROCESSOR arm)
set(CMAKE_C_COMPILER arm-none-eabi-gcc)
set(CMAKE_ASM_COMPILER arm-none-eabi-gcc)
# A full link cannot succeed during the compiler check without a runtime.
set(CMAKE_TRY_COMPILE_TARGET_TYPE STATIC_LIBRARY)
set(CMAKE_C_FLAGS_INIT "-mcpu=cortex-m4 -mthumb -ffreestanding")
set(CMAKE_EXE_LINKER_FLAGS_INIT "-nostdlib -nostartfiles -e main")
