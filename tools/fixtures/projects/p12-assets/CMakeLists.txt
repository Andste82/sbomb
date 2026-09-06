cmake_minimum_required(VERSION 3.20)
project(p12_assets C)
include(${CMAKE_CURRENT_LIST_DIR}/common.cmake)

fixture_executable(imgapp main.c)

# A generated asset: config.yaml is transformed into a blob that ends up inside
# the image. Only the generator relationship makes config.yaml an input; sitting
# next to the output would prove nothing (section 18).
add_custom_command(
  OUTPUT "${CMAKE_CURRENT_BINARY_DIR}/generated/config.bin"
  COMMAND "${CMAKE_COMMAND}" -E make_directory "${CMAKE_CURRENT_BINARY_DIR}/generated"
  COMMAND "${CMAKE_COMMAND}" -E copy
          "${CMAKE_CURRENT_SOURCE_DIR}/assets/config.yaml"
          "${CMAKE_CURRENT_BINARY_DIR}/generated/config.bin"
  DEPENDS "${CMAKE_CURRENT_SOURCE_DIR}/assets/config.yaml"
  COMMENT "Generating config.bin")

# The image: the application plus the generated blob plus a static asset.
add_custom_command(
  OUTPUT "${CMAKE_CURRENT_BINARY_DIR}/filesystem.img"
  COMMAND "${CMAKE_COMMAND}" -E cat
          "${CMAKE_CURRENT_BINARY_DIR}/generated/config.bin"
          "${CMAKE_CURRENT_SOURCE_DIR}/assets/index.html"
          > "${CMAKE_CURRENT_BINARY_DIR}/filesystem.img"
  DEPENDS "${CMAKE_CURRENT_BINARY_DIR}/generated/config.bin"
          "${CMAKE_CURRENT_SOURCE_DIR}/assets/index.html"
  VERBATIM
  COMMENT "Packing filesystem.img")

add_custom_target(image ALL DEPENDS "${CMAKE_CURRENT_BINARY_DIR}/filesystem.img")

# The build writes its own packaging manifest into the build tree, so the
# declaration is build evidence rather than something kept beside the sources.
file(WRITE "${CMAKE_CURRENT_BINARY_DIR}/sbomb-manifest.json"
"{\n"
"  \"schemaVersion\": 1,\n"
"  \"outputs\": [\n"
"    {\n"
"      \"path\": \"${CMAKE_CURRENT_BINARY_DIR}/filesystem.img\",\n"
"      \"kind\": \"image\",\n"
"      \"inputs\": [\n"
"        { \"path\": \"${CMAKE_CURRENT_BINARY_DIR}/generated/config.bin\", \"role\": \"generated-asset\",\n"
"          \"generatedFrom\": [\"assets/config.yaml\"], \"generator\": \"cmake -E copy\" },\n"
"        { \"path\": \"${CMAKE_CURRENT_SOURCE_DIR}/assets/index.html\", \"role\": \"asset\" }\n"
"      ]\n"
"    }\n"
"  ]\n"
"}\n")
