include_guard(GLOBAL)

include(CheckLinkerFlag)

set(SBOMB_LINK_EVIDENCE ON CACHE BOOL "Enable linker evidence flags for sbomb")
set(SBOMB_EXECUTABLE "sbomb" CACHE FILEPATH "Path to the sbomb executable")
set(SBOMB_OUTPUT_DIR "${CMAKE_BINARY_DIR}/sbom" CACHE PATH "Directory for sbomb output")

# The configuration used when sbomb_enable is called without CONFIG, and only
# when it exists.
#
# It is deliberately not called SBOMB_CONFIG. cmake_parse_arguments leaves
# SBOMB_CONFIG undefined when the caller passed no CONFIG, and an undefined
# normal variable lets a cache variable of the same name show through -- so a
# cache SBOMB_CONFIG would be passed as --config exactly as though the caller
# had asked for it, and every project without that file would fail at build
# time on a configuration it never named.
set(SBOMB_DEFAULT_CONFIG "${CMAKE_SOURCE_DIR}/sbomb.json"
    CACHE FILEPATH "Configuration used when sbomb_enable is called without CONFIG")

# Set here, at include() time, and not inside sbomb_enable.
#
# That is the whole reason the SBOM target used to re-configure the project.
# The generator decides whether to record a compile command when it processes
# the target, so switching this on afterwards -- which is when sbomb_enable
# runs, since the target has to exist before it can be named -- reaches the
# cache and misses the run: the compile database appeared only on the *next*
# configure. Set before any target is defined, one configure is enough.
#
# It follows that `include(Sbomb)` belongs above the targets it will be asked
# about. Below them, this arrives too late again, sbomb finds no compile
# database and says so with MISSING_COMPILE_EVIDENCE.
set(CMAKE_EXPORT_COMPILE_COMMANDS ON CACHE BOOL "Export compile commands for sbomb" FORCE)

# Being too late is silent otherwise: the build succeeds, the SBOM is written,
# and it is quietly worse because no object could be traced to a source. Say so
# at the moment it can still be moved.
get_property(_sbomb_existing_targets DIRECTORY PROPERTY BUILDSYSTEM_TARGETS)
if(_sbomb_existing_targets)
  message(WARNING
    "sbomb: include(Sbomb) comes after ${_sbomb_existing_targets}. "
    "CMAKE_EXPORT_COMPILE_COMMANDS then arrives too late for this run and "
    "compile_commands.json will be missing until the next configure, which "
    "costs sbomb the object-to-source evidence. Move the include above the "
    "targets.")
endif()
unset(_sbomb_existing_targets)

function(sbomb_enable)
  cmake_parse_arguments(SBOMB "" "TARGET;CONFIG;POLICY;MAP;DEPFILE;OUTPUT" "" ${ARGN})

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
  get_target_property(_sbomb_imported "${SBOMB_TARGET}" IMPORTED)
  if(_sbomb_type STREQUAL "INTERFACE_LIBRARY")
    message(WARNING "sbomb_enable ignored for INTERFACE library ${SBOMB_TARGET}")
    return()
  endif()

  # CMake 3.27 files the File API query for the run that is happening, so one
  # configure leaves a reply and the SBOM target has nothing to prepare.
  #
  # Below that the query is only read at the *start* of a run, so it takes
  # effect on the next one. There the target still re-configures, because a
  # reply that never arrives is not a degraded answer but no answer: sbomb
  # would know no targets, no anchors and no toolchain.
  set(_sbomb_reconfigure "")
  if(CMAKE_VERSION VERSION_GREATER_EQUAL "3.27")
    cmake_file_api(
      QUERY
      API_VERSION 1
      CODEMODEL 2
      CACHE 2
      CMAKEFILES 1
      TOOLCHAINS 1
    )
  else()
    file(MAKE_DIRECTORY "${CMAKE_BINARY_DIR}/.cmake/api/v1/query/client-sbomb")
    file(WRITE "${CMAKE_BINARY_DIR}/.cmake/api/v1/query/client-sbomb/query.json"
      "{\"requests\":[{\"kind\":\"codemodel\",\"version\":2},{\"kind\":\"cache\",\"version\":2},{\"kind\":\"cmakeFiles\",\"version\":1},{\"kind\":\"toolchains\",\"version\":1}]}\n")
    set(_sbomb_reconfigure
      COMMAND "${CMAKE_COMMAND}" -S "${CMAKE_SOURCE_DIR}" -B "${CMAKE_BINARY_DIR}")
  endif()

  # Only a target that is actually linked can carry linker flags. A static or
  # object library is archived rather than linked, and an imported target is
  # somebody else's build; target_link_options on either is at best ignored.
  if(SBOMB_LINK_EVIDENCE AND NOT _sbomb_imported
      AND _sbomb_type MATCHES "^(EXECUTABLE|SHARED_LIBRARY|MODULE_LIBRARY)$")
    get_property(_sbomb_language TARGET "${SBOMB_TARGET}" PROPERTY LINKER_LANGUAGE)
    if(NOT _sbomb_language)
      set(_sbomb_language C)
    endif()
    # MAP and DEPFILE say "the build already produces this, here it is" -- not
    # "write it here". Projects that set the flags in a toolchain file own
    # them, and adding ours beside theirs would put the option on the link line
    # twice, with the command-line order deciding which file wins.
    #
    # So a named path is passed to sbomb and nothing else happens. Whether it
    # is really there is not decided here: CMake offers too many ways to reach
    # the link line -- wrappers, response files, an overridden link rule -- for
    # a scan of the flags to answer it. sbomb checks the file itself and
    # refuses to run without it (CONFIGURED_EVIDENCE_MISSING).
    if(MSVC)
      if(NOT SBOMB_MAP)
        target_link_options("${SBOMB_TARGET}" PRIVATE "/MAP:$<TARGET_FILE:${SBOMB_TARGET}>.map")
      endif()
      target_link_options("${SBOMB_TARGET}" PRIVATE "/VERBOSE:LIB" "/VERBOSE:REF")
      if(NOT SBOMB_DEPFILE)
        message(STATUS "sbomb: link.exe has no dependency-file flag; skipping link dependency evidence for ${SBOMB_TARGET}")
      endif()
    else()
      if(NOT SBOMB_MAP)
        check_linker_flag(${_sbomb_language} "-Wl,-Map=<TARGET_FILE>.map" _sbomb_has_map)
        if(_sbomb_has_map)
          target_link_options("${SBOMB_TARGET}" PRIVATE "-Wl,-Map=$<TARGET_FILE:${SBOMB_TARGET}>.map")
        else()
          message(STATUS "sbomb: linker map flag unsupported; skipping map evidence for ${SBOMB_TARGET}")
        endif()
      endif()
      if(NOT SBOMB_DEPFILE)
        check_linker_flag(${_sbomb_language} "-Wl,--dependency-file=<TARGET_FILE>.d" _sbomb_has_depfile)
        if(_sbomb_has_depfile)
          target_link_options("${SBOMB_TARGET}" PRIVATE "-Wl,--dependency-file=$<TARGET_FILE:${SBOMB_TARGET}>.d")
        else()
          message(STATUS "sbomb: linker dependency-file flag unsupported; skipping link dependency evidence for ${SBOMB_TARGET}")
        endif()
      endif()
    endif()
  elseif(SBOMB_LINK_EVIDENCE)
    message(STATUS "sbomb: no linker evidence for ${SBOMB_TARGET}; a ${_sbomb_type} carries no linker flags")
  endif()

  # SBOMB_CONFIG is the parsed argument and nothing else. The default is
  # consulted only when the caller named none, and only when it is really
  # there: passing --config for a file that does not exist turns a run that
  # would have worked on defaults into a failure.
  set(_sbomb_config "${SBOMB_CONFIG}")
  if(_sbomb_config)
    if(NOT EXISTS "${_sbomb_config}")
      message(WARNING "sbomb_enable: CONFIG ${_sbomb_config} does not exist; the SBOM target will fail until it does")
    endif()
  elseif(EXISTS "${SBOMB_DEFAULT_CONFIG}")
    set(_sbomb_config "${SBOMB_DEFAULT_CONFIG}")
  endif()

  if(SBOMB_OUTPUT)
    set(_sbomb_output "${SBOMB_OUTPUT}")
  else()
    set(_sbomb_output "${SBOMB_OUTPUT_DIR}/${SBOMB_TARGET}.cdx.json")
  endif()
  get_filename_component(_sbomb_output_dir "${_sbomb_output}" DIRECTORY)
  if(NOT _sbomb_output_dir)
    set(_sbomb_output_dir ".")
  endif()

  set(_sbomb_command "${SBOMB_EXECUTABLE}" generate --build-dir "${CMAKE_BINARY_DIR}" --output "${_sbomb_output}")
  if(_sbomb_config)
    list(APPEND _sbomb_command --config "${_sbomb_config}")
  endif()
  if(SBOMB_POLICY)
    list(APPEND _sbomb_command --policy "${SBOMB_POLICY}")
  endif()
  # Without these the option would name a path nobody acts on: sbomb looks
  # beside the artifact and nowhere else unless it is told.
  if(SBOMB_MAP)
    list(APPEND _sbomb_command --map "${SBOMB_MAP}")
  endif()
  if(SBOMB_DEPFILE)
    list(APPEND _sbomb_command --link-depfile "${SBOMB_DEPFILE}")
  endif()
  add_custom_target("sbomb-${SBOMB_TARGET}"
    COMMAND "${CMAKE_COMMAND}" -E make_directory "${_sbomb_output_dir}"
    ${_sbomb_reconfigure}
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
