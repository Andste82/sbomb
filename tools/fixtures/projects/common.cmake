# Link evidence every fixture target must emit, so that the corpus exercises
# the depfile parser, the map parsers and DWARF inspection from one build.
if(MSVC)
  include(${CMAKE_CURRENT_LIST_DIR}/Sbomb.cmake)
endif()

function(fixture_executable target)
  add_executable(${target} ${ARGN})
  if(MSVC)
    sbomb_enable(TARGET ${target})
  else()
    target_link_options(${target} PRIVATE
      "-Wl,-Map=$<TARGET_FILE:${target}>.map"
      "-Wl,--dependency-file=$<TARGET_FILE:${target}>.d")
  endif()
  install(TARGETS ${target})
endfunction()
