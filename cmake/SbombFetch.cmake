include_guard(GLOBAL)

# Fetch the sbomb binary that belongs to this bundle.
#
# This file is meaningful inside the `sbomb-cmake.tar.gz` release asset, where
# the release script has substituted the version below. A copy taken from the
# source tree still carries the placeholder, and then the version has to be
# named explicitly with -DSBOMB_FETCH_VERSION=v0.12.0.

set(SBOMB_FETCH_VERSION "@SBOMB_VERSION@"
    CACHE STRING "sbomb release to fetch, e.g. v0.12.0")
set(SBOMB_FETCH_REPOSITORY "Andste82/sbomb"
    CACHE STRING "Repository the release is fetched from")

# _sbomb_host_asset names the release asset for the machine running the build.
#
# The host, not the target. sbomb reads a build tree and runs wherever cmake
# runs: a firmware project cross-compiling to bare-metal ARM still needs the
# binary for the developer's laptop, and CMAKE_SYSTEM_PROCESSOR would name the
# microcontroller.
function(_sbomb_host_asset out_asset)
  if(CMAKE_HOST_SYSTEM_NAME STREQUAL "Linux")
    set(_os "linux")
  elseif(CMAKE_HOST_SYSTEM_NAME STREQUAL "Darwin")
    set(_os "darwin")
  elseif(CMAKE_HOST_SYSTEM_NAME STREQUAL "Windows")
    set(_os "windows")
  else()
    set(${out_asset} "" PARENT_SCOPE)
    return()
  endif()

  string(TOLOWER "${CMAKE_HOST_SYSTEM_PROCESSOR}" _machine)
  if(_machine MATCHES "^(x86_64|amd64)$")
    set(_arch "amd64")
  elseif(_machine MATCHES "^(aarch64|arm64)$")
    set(_arch "arm64")
  else()
    set(${out_asset} "" PARENT_SCOPE)
    return()
  endif()

  # Windows is published for amd64 only, and arm64 Windows runs it anyway.
  if(_os STREQUAL "windows")
    set(${out_asset} "sbomb-windows-amd64.exe" PARENT_SCOPE)
  else()
    set(${out_asset} "sbomb-${_os}-${_arch}" PARENT_SCOPE)
  endif()
endfunction()

# sbomb_fetch_binary downloads the release binary and points SBOMB_EXECUTABLE at
# it, unless the caller has already named one.
#
# The download is checked against the release's SHA256SUMS. There is no option
# to skip that, for the same reason the install scripts have none: a tool whose
# argument is that you should be able to verify what you were given cannot hand
# you a binary it did not verify itself.
function(sbomb_fetch_binary)
  # A path the caller set wins; "sbomb" is the module's own default and means
  # nobody has chosen. Both sides are quoted on purpose: an undefined variable
  # left bare in if() is compared as the literal string "SBOMB_EXECUTABLE", so
  # the unquoted form silently decided that a name nobody had set was a path
  # somebody had.
  if(NOT "${SBOMB_EXECUTABLE}" STREQUAL "sbomb" AND NOT "${SBOMB_EXECUTABLE}" STREQUAL "")
    message(STATUS "sbomb: using ${SBOMB_EXECUTABLE}, not fetching")
    return()
  endif()

  if(SBOMB_FETCH_VERSION MATCHES "^@.*@$")
    message(FATAL_ERROR
      "sbomb: this bundle carries no version. It is meant to be fetched as the "
      "sbomb-cmake.tar.gz asset of a release; from a source checkout, pass "
      "-DSBOMB_FETCH_VERSION=v0.12.0 or -DSBOMB_EXECUTABLE=<path>.")
  endif()

  _sbomb_host_asset(_asset)
  if(NOT _asset)
    message(FATAL_ERROR
      "sbomb: no release binary for ${CMAKE_HOST_SYSTEM_NAME}/${CMAKE_HOST_SYSTEM_PROCESSOR}; "
      "build it with `go build ./cmd/sbomb` and pass -DSBOMB_EXECUTABLE=<path>.")
  endif()

  set(_dir "${CMAKE_BINARY_DIR}/_sbomb/${SBOMB_FETCH_VERSION}")
  set(_binary "${_dir}/${_asset}")
  set(_base "https://github.com/${SBOMB_FETCH_REPOSITORY}/releases/download/${SBOMB_FETCH_VERSION}")

  if(NOT EXISTS "${_binary}")
    file(MAKE_DIRECTORY "${_dir}")

    message(STATUS "sbomb: fetching ${_asset} ${SBOMB_FETCH_VERSION}")
    file(DOWNLOAD "${_base}/${_asset}" "${_binary}.part" STATUS _status TLS_VERIFY ON)
    list(GET _status 0 _code)
    if(NOT _code EQUAL 0)
      list(GET _status 1 _message)
      file(REMOVE "${_binary}.part")
      message(FATAL_ERROR "sbomb: could not download ${_asset} for ${SBOMB_FETCH_VERSION}: ${_message}")
    endif()

    file(DOWNLOAD "${_base}/SHA256SUMS" "${_dir}/SHA256SUMS" STATUS _status TLS_VERIFY ON)
    list(GET _status 0 _code)
    if(NOT _code EQUAL 0)
      file(REMOVE "${_binary}.part")
      message(FATAL_ERROR "sbomb: the release has no SHA256SUMS, so the download cannot be verified")
    endif()

    # Compare the digest of what arrived against the line for this asset, so
    # that a matching line for some other file proves nothing.
    file(READ "${_dir}/SHA256SUMS" _sums)
    string(REPLACE "." "\\." _pattern "${_asset}")
    # CMake's regex flavour has no bounded repetition, so the digest is matched
    # as "one or more hex digits" rather than as exactly sixty-four of them.
    # Requiring the line to end right after the name is what keeps
    # sbomb-linux-amd64 from matching sbomb-linux-amd64.cdx.json.
    if(NOT _sums MATCHES "([0-9a-f]+)[ \t]+\\*?${_pattern}[\r\n]")
      file(REMOVE "${_binary}.part")
      message(FATAL_ERROR "sbomb: SHA256SUMS names no checksum for ${_asset}")
    endif()
    set(_expected "${CMAKE_MATCH_1}")
    file(SHA256 "${_binary}.part" _actual)
    if(NOT _actual STREQUAL _expected)
      file(REMOVE "${_binary}.part")
      message(FATAL_ERROR "sbomb: checksum mismatch for ${_asset}: expected ${_expected}, got ${_actual}")
    endif()

    # Renamed only once it is verified, so an interrupted configure cannot
    # leave a half-written binary that the next run treats as cached.
    file(RENAME "${_binary}.part" "${_binary}")
    if(NOT CMAKE_HOST_SYSTEM_NAME STREQUAL "Windows")
      file(CHMOD "${_binary}" PERMISSIONS
        OWNER_READ OWNER_WRITE OWNER_EXECUTE
        GROUP_READ GROUP_EXECUTE
        WORLD_READ WORLD_EXECUTE)
    endif()
    message(STATUS "sbomb: checksum verified")
  endif()

  set(SBOMB_EXECUTABLE "${_binary}" CACHE FILEPATH "Path to the sbomb executable" FORCE)
  message(STATUS "sbomb: using ${_binary}")
endfunction()
