# Link evidence every fixture target must emit, so that the corpus exercises
# the depfile parser, the map parsers and DWARF inspection from one build.
function(fixture_executable target)
  add_executable(${target} ${ARGN})
  target_link_options(${target} PRIVATE
    "-Wl,-Map=$<TARGET_FILE:${target}>.map"
    "-Wl,--dependency-file=$<TARGET_FILE:${target}>.d")
  install(TARGETS ${target})
endfunction()
