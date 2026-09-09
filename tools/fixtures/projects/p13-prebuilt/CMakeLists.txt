cmake_minimum_required(VERSION 3.20)
project(p13_prebuilt C)
include(${CMAKE_CURRENT_LIST_DIR}/common.cmake)

# A prebuilt archive, the way a vendor ships one: compiled and archived at
# configure time, so nothing in build.ninja, compile_commands.json or any
# depfile records how it was made. Its members can be resolved to their source
# only by reading the debug information inside them, which is strategy 6 of
# section 13.2.
set(prebuilt_dir "${CMAKE_CURRENT_BINARY_DIR}/prebuilt")
file(MAKE_DIRECTORY "${prebuilt_dir}")
if(MSVC)
  set(prebuilt_object "${prebuilt_dir}/vendor_blob.obj")
  set(prebuilt_archive "${prebuilt_dir}/libvendor.lib")
  execute_process(
    COMMAND "${CMAKE_C_COMPILER}" /nologo /c /Zi
            "${CMAKE_CURRENT_SOURCE_DIR}/vendor_blob.c"
            "/Fo${prebuilt_object}"
    COMMAND_ERROR_IS_FATAL ANY)
  execute_process(
    COMMAND "${CMAKE_AR}" /nologo "/OUT:${prebuilt_archive}" "${prebuilt_object}"
    COMMAND_ERROR_IS_FATAL ANY)
else()
  set(prebuilt_object "${prebuilt_dir}/vendor_blob.o")
  set(prebuilt_archive "${prebuilt_dir}/libvendor.a")
  execute_process(
    COMMAND "${CMAKE_C_COMPILER}" -g -c "${CMAKE_CURRENT_SOURCE_DIR}/vendor_blob.c"
            -o "${prebuilt_object}"
    COMMAND_ERROR_IS_FATAL ANY)
  execute_process(
    COMMAND "${CMAKE_AR}" rcs "${prebuilt_archive}" "${prebuilt_object}"
    COMMAND_ERROR_IS_FATAL ANY)
endif()

fixture_executable(prebuiltapp main.c)
target_link_libraries(prebuiltapp PRIVATE "${prebuilt_archive}")
