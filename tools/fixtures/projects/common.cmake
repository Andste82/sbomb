# Link evidence every fixture target must emit, so that the corpus exercises
# the depfile parser, the map parsers and DWARF inspection from one build.
function(fixture_executable target)
  add_executable(${target} ${ARGN})
  if(MSVC)
    target_link_options(${target} PRIVATE
      "/MAP:$<TARGET_FILE:${target}>.map")
  else()
    target_link_options(${target} PRIVATE
      "-Wl,-Map=$<TARGET_FILE:${target}>.map"
      "-Wl,--dependency-file=$<TARGET_FILE:${target}>.d")
  endif()
  install(TARGETS ${target})
endfunction()
