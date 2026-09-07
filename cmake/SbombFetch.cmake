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

# The CA bundle the downloads are verified against.
#
# The peer is always checked. Behind a TLS-intercepting proxy that check fails
# against the proxy's certificate rather than GitHub's, and naming the CA that
# signed it is the fix -- switching the verification off is not, because
# SHA256SUMS arrives over the same connection as the binary it vouches for, and
# a checksum an attacker also supplied proves nothing.
set(SBOMB_FETCH_TLS_CAINFO "" CACHE FILEPATH
    "CA bundle used to verify the download, e.g. the root a TLS-intercepting proxy re-signs with")

# Pinning: naming the digest the caller expects, rather than taking the one the
# release hands over beside the file.
#
# The download is always checked. Unpinned, it is checked against the
# SHA256SUMS of the same release -- which catches a truncated or corrupted
# transfer, but is trust on first use: whoever could substitute the binary
# could substitute the list. A digest that came from somewhere else -- the
# release notes, a colleague, a previous audit -- is a claim the network cannot
# make, and that is what these two are for.
#
# SBOMB_FETCH_SHA256 pins this host's binary; SBOMB_FETCH_SHA256SUMS pins the
# list, which covers every host with one value. Both are the digests published
# in the release's own SHA256SUMS.
set(SBOMB_FETCH_SHA256 "" CACHE STRING
    "Expected SHA-256 of the host's sbomb binary; when set, SHA256SUMS is not fetched")
set(SBOMB_FETCH_SHA256SUMS "" CACHE STRING
    "Expected SHA-256 of the release's SHA256SUMS file; pins every asset with one value")

# _sbomb_expect_digest normalises a pinned digest and refuses anything that is
# not one.
#
# A typo would otherwise arrive as a checksum mismatch, which reads like the
# download was tampered with -- the one message that must never be spent on a
# value the caller simply mistyped.
function(_sbomb_expect_digest value origin out_digest)
  string(TOLOWER "${value}" _digest)
  string(STRIP "${_digest}" _digest)
  # The length is counted rather than matched: CMake's regex flavour has no
  # bounded repetition, so "[0-9a-f]{64}" is not available here.
  string(LENGTH "${_digest}" _length)
  if(NOT _digest MATCHES "^[0-9a-f]+$" OR NOT _length EQUAL 64)
    message(FATAL_ERROR
      "sbomb: ${origin} is not a SHA-256 digest: expected 64 hex characters, "
      "got '${value}'.")
  endif()
  set(${out_digest} "${_digest}" PARENT_SCOPE)
endfunction()

# _sbomb_cainfo answers which CA bundle to use, and where it was named.
#
# The cache variable first, then CMake's own CMAKE_TLS_CAINFO, then the two
# environment variables such a proxy usually already exports -- so an
# intercepted machine that is set up for curl and OpenSSL needs nothing here.
function(_sbomb_cainfo out_file out_origin)
  # Written out rather than looped over a list of name/value pairs: a pair whose
  # value is empty collapses to a one-element list, and list(GET) on the missing
  # second element is an error rather than an empty string.
  if(SBOMB_FETCH_TLS_CAINFO)
    set(_file "${SBOMB_FETCH_TLS_CAINFO}")
    set(_origin "SBOMB_FETCH_TLS_CAINFO")
  elseif(CMAKE_TLS_CAINFO)
    set(_file "${CMAKE_TLS_CAINFO}")
    set(_origin "CMAKE_TLS_CAINFO")
  elseif(NOT "$ENV{SSL_CERT_FILE}" STREQUAL "")
    set(_file "$ENV{SSL_CERT_FILE}")
    set(_origin "the SSL_CERT_FILE environment variable")
  elseif(NOT "$ENV{CURL_CA_BUNDLE}" STREQUAL "")
    set(_file "$ENV{CURL_CA_BUNDLE}")
    set(_origin "the CURL_CA_BUNDLE environment variable")
  else()
    set(${out_file} "" PARENT_SCOPE)
    set(${out_origin} "" PARENT_SCOPE)
    return()
  endif()

  # Said here, once, rather than as a download failure per file. A bundle that
  # is not there cannot verify anything, and the likeliest reason to reach this
  # is a path typed by hand.
  if(NOT EXISTS "${_file}")
    message(FATAL_ERROR
      "sbomb: ${_origin} names ${_file}, which does not exist; "
      "no CA bundle, no verified download.")
  endif()

  set(${out_file} "${_file}" PARENT_SCOPE)
  set(${out_origin} "${_origin}" PARENT_SCOPE)
endfunction()

# _sbomb_download fetches one URL and reports what actually went wrong.
#
# It sets out_error to the empty string on success, and to libcurl's own
# message otherwise. The old code threw that message away for SHA256SUMS and
# announced that the release did not have the file -- a claim about the
# release, made on the evidence of a failed request, which sent whoever read it
# looking in the wrong place.
#
# Three attempts, because a single refused request is not an answer about
# anything: three CI jobs pulling one release at the same moment is enough for
# one of them to be throttled, and that configure would have worked a second
# later.
function(_sbomb_download url out_file what out_error)
  _sbomb_cainfo(_ca _ca_origin)
  set(_tls TLS_VERIFY ON)
  if(_ca)
    list(APPEND _tls TLS_CAINFO "${_ca}")
  endif()

  set(_attempts 3)
  foreach(_try RANGE 1 ${_attempts})
    file(DOWNLOAD "${url}" "${out_file}" STATUS _status INACTIVITY_TIMEOUT 60 ${_tls})
    list(GET _status 0 _code)
    if(_code EQUAL 0)
      set(${out_error} "" PARENT_SCOPE)
      return()
    endif()
    list(GET _status 1 _message)
    if(_try LESS _attempts)
      message(STATUS "sbomb: ${what} failed (${_message}); retrying")
      math(EXPR _pause "${_try} * 2")
      execute_process(COMMAND "${CMAKE_COMMAND}" -E sleep ${_pause})
    endif()
  endforeach()

  # A certificate error behind an intercepting proxy is the one failure whose
  # message does not suggest its own fix, so it gets told.
  if(_message MATCHES "[Cc]ertificate|SSL|TLS")
    if(_ca)
      set(_message "${_message} (verified against ${_ca}, named by ${_ca_origin})")
    else()
      set(_message
        "${_message} -- if this network intercepts TLS, name the CA it re-signs with: -DSBOMB_FETCH_TLS_CAINFO=<file>")
    endif()
  endif()
  set(${out_error} "${_message}" PARENT_SCOPE)
endfunction()

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
    _sbomb_cainfo(_ca _ca_origin)
    if(_ca)
      message(STATUS "sbomb: verifying the connection against ${_ca} (${_ca_origin})")
    endif()

    _sbomb_download("${_base}/${_asset}" "${_binary}.part" "${_asset}" _error)
    if(_error)
      file(REMOVE "${_binary}.part")
      message(FATAL_ERROR "sbomb: could not download ${_asset} for ${SBOMB_FETCH_VERSION}: ${_error}")
    endif()

    if(SBOMB_FETCH_SHA256)
      # A pinned digest is the caller's own claim about which bytes they mean,
      # so SHA256SUMS is not fetched at all: a list downloaded beside the file
      # it vouches for can only confirm what the same connection already
      # supplied, and it cannot outrank a value that came from somewhere else.
      _sbomb_expect_digest("${SBOMB_FETCH_SHA256}" "SBOMB_FETCH_SHA256" _expected)
      set(_expected_from "SBOMB_FETCH_SHA256")
    else()
      _sbomb_download("${_base}/SHA256SUMS" "${_dir}/SHA256SUMS" "SHA256SUMS" _error)
      if(_error)
        file(REMOVE "${_binary}.part")
        message(FATAL_ERROR
          "sbomb: could not download SHA256SUMS for ${SBOMB_FETCH_VERSION}, "
          "so the download cannot be verified: ${_error}")
      endif()

      # Pinning the list pins every asset in it, which is the one value a mixed
      # team can share: the per-asset digests below are then read out of a file
      # that has itself been checked against something the network did not say.
      if(SBOMB_FETCH_SHA256SUMS)
        _sbomb_expect_digest("${SBOMB_FETCH_SHA256SUMS}" "SBOMB_FETCH_SHA256SUMS" _want_sums)
        file(SHA256 "${_dir}/SHA256SUMS" _got_sums)
        if(NOT _got_sums STREQUAL _want_sums)
          file(REMOVE "${_binary}.part" "${_dir}/SHA256SUMS")
          message(FATAL_ERROR
            "sbomb: SHA256SUMS for ${SBOMB_FETCH_VERSION} does not match "
            "SBOMB_FETCH_SHA256SUMS: expected ${_want_sums}, got ${_got_sums}")
        endif()
        message(STATUS "sbomb: SHA256SUMS matches the pinned digest")
      endif()

      # Compare the digest of what arrived against the line for this asset, so
      # that a matching line for some other file proves nothing.
      file(READ "${_dir}/SHA256SUMS" _sums)
      string(REPLACE "." "\\." _pattern "${_asset}")
      # CMake's regex flavour has no bounded repetition, so the digest is
      # matched as "one or more hex digits" rather than as exactly sixty-four
      # of them. Requiring the line to end right after the name is what keeps
      # sbomb-linux-amd64 from matching sbomb-linux-amd64.cdx.json.
      if(NOT _sums MATCHES "([0-9a-f]+)[ \t]+\\*?${_pattern}[\r\n]")
        file(REMOVE "${_binary}.part")
        message(FATAL_ERROR "sbomb: SHA256SUMS names no checksum for ${_asset}")
      endif()
      set(_expected "${CMAKE_MATCH_1}")
      set(_expected_from "the release's SHA256SUMS")
    endif()

    file(SHA256 "${_binary}.part" _actual)
    if(NOT _actual STREQUAL _expected)
      file(REMOVE "${_binary}.part")
      message(FATAL_ERROR
        "sbomb: checksum mismatch for ${_asset} against ${_expected_from}: "
        "expected ${_expected}, got ${_actual}")
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
