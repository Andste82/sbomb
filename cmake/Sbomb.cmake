include_guard(GLOBAL)

# 3.27 is where cmake_file_api() files the query for the run in progress. Below
# that the query is only read at the start of a run, so a first configure
# leaves no reply and sbomb knows no targets, no anchors and no toolchain.
#
# This is a check and not cmake_minimum_required(): this file is include()d, so
# that call would apply to whoever included it and silently raise the policy
# version of a project that asked for an older one.
if(CMAKE_VERSION VERSION_LESS "3.27")
  message(FATAL_ERROR
    "sbomb requires CMake 3.27 or newer for the File API query; this is ${CMAKE_VERSION}")
endif()

include(CheckLinkerFlag)

# None of these is named after an sbomb_enable keyword, and none may be:
# cmake_parse_arguments clears SBOMB_<KEYWORD> for every keyword a call omits,
# and a cache variable of that name shows through the cleared one -- reaching
# sbomb as though the call had named it. A cache variable that stands in for a
# keyword outright carries the SBOMB_DEFAULT_ prefix; SBOMB_OUTPUT_DIR is a
# setting of its own, naming the directory rather than the file OUTPUT names.
set(SBOMB_LINK_EVIDENCE ON CACHE BOOL "Enable linker evidence flags for sbomb")
set(SBOMB_EXECUTABLE "sbomb" CACHE FILEPATH "Path to the sbomb executable")
set(SBOMB_OUTPUT_DIR "${CMAKE_BINARY_DIR}/sbom" CACHE PATH "Directory for sbomb output")
set(SBOMB_DEFAULT_CONFIG "${CMAKE_SOURCE_DIR}/sbomb.json"
    CACHE FILEPATH "Configuration used when sbomb_enable is called without CONFIG")
set(SBOMB_DEFAULT_FOSS_OUT ""
    CACHE PATH "FOSS attribution output directory used when sbomb_enable is called without FOSS_OUT")
set(SBOMB_DEFAULT_FOSS_FORMAT ""
    CACHE STRING "FOSS rendering used when sbomb_enable is called without FOSS_FORMAT: text or markdown")

# Set at include() time, before any target is defined, so that one configure
# is enough. The generator decides whether to record a compile command as it
# processes a target; a value arriving after that -- which is when sbomb_enable
# runs, since a target must exist before it can be named -- reaches the cache
# but misses the run, and the compile database appears only on the next
# configure.
#
# So `include(Sbomb)` belongs above the targets it is asked about. Below them
# sbomb finds no compile database and says so with MISSING_COMPILE_EVIDENCE.
set(CMAKE_EXPORT_COMPILE_COMMANDS ON CACHE BOOL "Export compile commands for sbomb" FORCE)

# Too late is otherwise silent: the build succeeds and the SBOM is written,
# only with no object traced to a source. Say so while the include can still be
# moved.
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
  set(_sbomb_keywords TARGET CONFIG POLICY MAP DEPFILE OUTPUT FOSS_OUT FOSS_FORMAT)
  cmake_parse_arguments(SBOMB "" "${_sbomb_keywords}" "" ${ARGN})

  # cmake_parse_arguments clears SBOMB_<KEYWORD> for every keyword this call
  # omitted, and clearing a normal variable is what lets a cache variable of
  # the same name show through it. The names above are therefore taken from
  # the call and from nowhere else: each keyword the call did not pass is set
  # empty here, covering any cache entry standing behind it.
  #
  # ARGN is walked the way the parser reads it: a keyword counts as passed only
  # once a value that is not itself a keyword follows it. The parser never
  # swallows a keyword as the previous one's value -- in CONFIG OUTPUT it
  # leaves both without one -- and a walk that disagreed would call a keyword
  # passed that the parser had cleared, and leave it unguarded.
  set(_sbomb_named "")
  set(_sbomb_pending "")
  foreach(_sbomb_arg IN LISTS ARGN)
    list(FIND _sbomb_keywords "${_sbomb_arg}" _sbomb_at)
    if(NOT _sbomb_at EQUAL -1)
      set(_sbomb_pending "${_sbomb_arg}")
    elseif(NOT _sbomb_pending STREQUAL "")
      list(APPEND _sbomb_named "${_sbomb_pending}")
      set(_sbomb_pending "")
    endif()
  endforeach()
  foreach(_sbomb_keyword IN LISTS _sbomb_keywords)
    list(FIND _sbomb_named "${_sbomb_keyword}" _sbomb_at)
    if(_sbomb_at EQUAL -1)
      set("SBOMB_${_sbomb_keyword}" "")
    endif()
  endforeach()

  if(NOT SBOMB_TARGET)
    message(FATAL_ERROR "sbomb_enable requires TARGET <target>")
  endif()
  if(NOT TARGET "${SBOMB_TARGET}")
    message(FATAL_ERROR "sbomb_enable target does not exist: ${SBOMB_TARGET}")
  endif()

  # Settled before the returns below, so that a call is answered the same way
  # whatever kind of target it names: an ALIAS or an INTERFACE library is
  # skipped, and skipping it must not swallow a FOSS_FORMAT that is not a
  # rendering at all.
  #
  # The parsed arguments first; the defaults only when the call named none. The
  # rendering default needs an output to act on, so on its own it stays inert
  # rather than refusing every call at the check below.
  set(_sbomb_foss_out "${SBOMB_FOSS_OUT}")
  if(NOT _sbomb_foss_out)
    set(_sbomb_foss_out "${SBOMB_DEFAULT_FOSS_OUT}")
  endif()
  set(_sbomb_foss_format "${SBOMB_FOSS_FORMAT}")
  if(NOT _sbomb_foss_format AND _sbomb_foss_out)
    set(_sbomb_foss_format "${SBOMB_DEFAULT_FOSS_FORMAT}")
  endif()

  # A rendering with no output to write has nothing to act on. Only a call that
  # passed FOSS_FORMAT reaches this.
  if(_sbomb_foss_format AND NOT _sbomb_foss_out)
    message(FATAL_ERROR "sbomb_enable: FOSS_FORMAT requires FOSS_OUT")
  endif()
  if(_sbomb_foss_format AND NOT _sbomb_foss_format STREQUAL "text" AND NOT _sbomb_foss_format STREQUAL "markdown")
    message(FATAL_ERROR "sbomb_enable: invalid FOSS_FORMAT '${_sbomb_foss_format}'; use text or markdown")
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

  # The version check at the top of this file guarantees the File API query is
  # filed for the run in progress, so this same configure leaves a reply and
  # the SBOM target never needs to reconfigure for one.
  cmake_file_api(
    QUERY
    API_VERSION 1
    CODEMODEL 2
    CACHE 2
    CMAKEFILES 1
    TOOLCHAINS 1
  )

  # Only a target that is actually linked can carry linker flags. A static or
  # object library is archived rather than linked, and an imported target is
  # somebody else's build; target_link_options on either is at best ignored.
  if(SBOMB_LINK_EVIDENCE AND NOT _sbomb_imported
      AND _sbomb_type MATCHES "^(EXECUTABLE|SHARED_LIBRARY|MODULE_LIBRARY)$")
    get_property(_sbomb_language TARGET "${SBOMB_TARGET}" PROPERTY LINKER_LANGUAGE)
    if(NOT _sbomb_language)
      set(_sbomb_language C)
    endif()
    # MAP and DEPFILE name a file the build already produces; they do not ask
    # for one to be written. A project setting these flags in a toolchain file
    # owns them, so no flag of ours joins them: the same option twice on the
    # link line leaves command-line order to decide which file wins.
    #
    # A named path is therefore passed to sbomb and nothing else happens.
    # Whether the file is really there is not decided here -- wrappers,
    # response files and overridden link rules put too much out of reach of a
    # scan of the flags. sbomb checks the file itself and refuses to run
    # without it (CONFIGURED_EVIDENCE_MISSING).
    # check_linker_flag caches its answer under the name it is given and skips
    # the test when that name is set. The flag it tests changed -- it used to
    # read <TARGET_FILE>.map, which is not a generator expression and reached
    # the linker as those literal characters -- so the names change with it.
    # Otherwise a tree configured before that fix keeps the answer the broken
    # flag produced: on Windows < and > cannot occur in a filename, the test
    # failed there, and the cached failure would outlive its cause.
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
        check_linker_flag(${_sbomb_language} "-Wl,-Map=sbomb_check.map" _sbomb_map_flag_supported)
        if(_sbomb_map_flag_supported)
          target_link_options("${SBOMB_TARGET}" PRIVATE "-Wl,-Map=$<TARGET_FILE:${SBOMB_TARGET}>.map")
        else()
          message(STATUS "sbomb: linker map flag unsupported; skipping map evidence for ${SBOMB_TARGET}")
        endif()
      endif()
      if(NOT SBOMB_DEPFILE)
        check_linker_flag(${_sbomb_language} "-Wl,--dependency-file=sbomb_check.d" _sbomb_depfile_flag_supported)
        if(_sbomb_depfile_flag_supported)
          target_link_options("${SBOMB_TARGET}" PRIVATE "-Wl,--dependency-file=$<TARGET_FILE:${SBOMB_TARGET}>.d")
        else()
          message(STATUS "sbomb: linker dependency-file flag unsupported; skipping link dependency evidence for ${SBOMB_TARGET}")
        endif()
      endif()
    endif()
  elseif(SBOMB_LINK_EVIDENCE)
    message(STATUS "sbomb: no linker evidence for ${SBOMB_TARGET}; a ${_sbomb_type} carries no linker flags")
  endif()

  # The parsed argument first; the default only when the call named none, and
  # only when the file is really there. --config for a file that does not exist
  # fails a run that defaults alone would have carried.
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
  # sbomb looks beside the artifact and nowhere else unless it is told, so a
  # path the call named has to be passed on.
  if(SBOMB_MAP)
    list(APPEND _sbomb_command --map "${SBOMB_MAP}")
  endif()
  if(SBOMB_DEPFILE)
    list(APPEND _sbomb_command --link-depfile "${SBOMB_DEPFILE}")
  endif()
  if(_sbomb_foss_out)
    list(APPEND _sbomb_command --foss-out "${_sbomb_foss_out}")
  endif()
  if(_sbomb_foss_format)
    list(APPEND _sbomb_command --foss-format "${_sbomb_foss_format}")
  endif()
  add_custom_target("sbomb-${SBOMB_TARGET}"
    COMMAND "${CMAKE_COMMAND}" -E make_directory "${_sbomb_output_dir}"
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
