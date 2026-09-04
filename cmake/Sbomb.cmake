include_guard(GLOBAL)

include(CheckLinkerFlag)

set(SBOMB_LINK_EVIDENCE ON CACHE BOOL "Enable linker evidence flags for sbomb")
set(SBOMB_EXECUTABLE "sbomb" CACHE FILEPATH "Path to the sbomb executable")
set(SBOMB_OUTPUT_DIR "${CMAKE_BINARY_DIR}/sbom" CACHE PATH "Directory for sbomb output")

function(sbomb_enable)
  cmake_parse_arguments(SBOMB "" "TARGET;CONFIG;POLICY" "" ${ARGN})

  if(NOT SBOMB_TARGET)
    message(FATAL_ERROR "sbomb_enable requires TARGET <target>")
  endif()
  if(NOT TARGET "${SBOMB_TARGET}")
    message(FATAL_ERROR "sbomb_enable target does not exist: ${SBOMB_TARGET}")
  endif()

  get_target_property(_sbomb_alias "${SBOMB_TARGET}" ALIASED_TARGET)
  if(_sbomb_alias)
    message(WARNING "sbomb_enable ignored for ALIAS target ${SBOMB_TARGET}")
    return()
  endif()
  get_target_property(_sbomb_type "${SBOMB_TARGET}" TYPE)
  if(_sbomb_type STREQUAL "INTERFACE_LIBRARY")
    message(WARNING "sbomb_enable ignored for INTERFACE library ${SBOMB_TARGET}")
    return()
  endif()

  set(CMAKE_EXPORT_COMPILE_COMMANDS ON)
  set(CMAKE_EXPORT_COMPILE_COMMANDS ON CACHE BOOL "Export compile commands for sbomb" FORCE)
  file(MAKE_DIRECTORY "${CMAKE_BINARY_DIR}/.cmake/api/v1/query/client-sbomb")
  file(WRITE "${CMAKE_BINARY_DIR}/.cmake/api/v1/query/client-sbomb/query.json"
    "{\"requests\":[{\"kind\":\"codemodel\",\"version\":2},{\"kind\":\"cache\",\"version\":2},{\"kind\":\"cmakeFiles\",\"version\":1},{\"kind\":\"toolchains\",\"version\":1}]}\n")

  if(SBOMB_LINK_EVIDENCE)
    get_property(_sbomb_language TARGET "${SBOMB_TARGET}" PROPERTY LINKER_LANGUAGE)
    if(NOT _sbomb_language)
      set(_sbomb_language C)
    endif()
    check_linker_flag(${_sbomb_language} "-Wl,-Map=<TARGET_FILE>.map" _sbomb_has_map)
    check_linker_flag(${_sbomb_language} "-Wl,--dependency-file=<TARGET_FILE>.d" _sbomb_has_depfile)
    if(_sbomb_has_map)
      target_link_options("${SBOMB_TARGET}" PRIVATE "-Wl,-Map=$<TARGET_FILE:${SBOMB_TARGET}>.map")
    else()
      message(STATUS "sbomb: linker map flag unsupported; skipping map evidence for ${SBOMB_TARGET}")
    endif()
    if(_sbomb_has_depfile)
      target_link_options("${SBOMB_TARGET}" PRIVATE "-Wl,--dependency-file=$<TARGET_FILE:${SBOMB_TARGET}>.d")
    else()
      message(STATUS "sbomb: linker dependency-file flag unsupported; skipping link dependency evidence for ${SBOMB_TARGET}")
    endif()
  endif()

  set(_sbomb_command "${SBOMB_EXECUTABLE}" generate --build-dir "${CMAKE_BINARY_DIR}" --output "${SBOMB_OUTPUT_DIR}/${SBOMB_TARGET}.cdx.json")
  if(SBOMB_CONFIG)
    list(APPEND _sbomb_command --config "${SBOMB_CONFIG}")
  endif()
  if(SBOMB_POLICY)
    list(APPEND _sbomb_command --policy "${SBOMB_POLICY}")
  endif()
  add_custom_target("sbomb-${SBOMB_TARGET}"
    COMMAND "${CMAKE_COMMAND}" -E make_directory "${SBOMB_OUTPUT_DIR}"
    COMMAND "${CMAKE_COMMAND}" -S "${CMAKE_SOURCE_DIR}" -B "${CMAKE_BINARY_DIR}" -DCMAKE_EXPORT_COMPILE_COMMANDS=ON
    COMMAND ${_sbomb_command}
    WORKING_DIRECTORY "${CMAKE_SOURCE_DIR}"
    DEPENDS "${SBOMB_TARGET}"
    USES_TERMINAL
  )
  set_property(TARGET "sbomb-${SBOMB_TARGET}" PROPERTY EXCLUDE_FROM_ALL TRUE)
  if(NOT TARGET sbomb)
    add_custom_target(sbomb)
    set_property(TARGET sbomb PROPERTY EXCLUDE_FROM_ALL TRUE)
  endif()
  add_dependencies(sbomb "sbomb-${SBOMB_TARGET}")
endfunction()