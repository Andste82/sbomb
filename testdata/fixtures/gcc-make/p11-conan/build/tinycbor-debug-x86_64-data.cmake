########### AGGREGATED COMPONENTS AND DEPENDENCIES FOR THE MULTI CONFIG #####################
#############################################################################################

set(tinycbor_COMPONENT_NAMES "")
if(DEFINED tinycbor_FIND_DEPENDENCY_NAMES)
  list(APPEND tinycbor_FIND_DEPENDENCY_NAMES )
  list(REMOVE_DUPLICATES tinycbor_FIND_DEPENDENCY_NAMES)
else()
  set(tinycbor_FIND_DEPENDENCY_NAMES )
endif()

########### VARIABLES #######################################################################
#############################################################################################
set(tinycbor_PACKAGE_FOLDER_DEBUG "/__fixture_pkg__/conan/p/b/tinyc0d8a84e35b4eb/p")
set(tinycbor_BUILD_MODULES_PATHS_DEBUG )


set(tinycbor_INCLUDE_DIRS_DEBUG "${tinycbor_PACKAGE_FOLDER_DEBUG}/include")
set(tinycbor_RES_DIRS_DEBUG )
set(tinycbor_DEFINITIONS_DEBUG )
set(tinycbor_SHARED_LINK_FLAGS_DEBUG )
set(tinycbor_EXE_LINK_FLAGS_DEBUG )
set(tinycbor_OBJECTS_DEBUG )
set(tinycbor_COMPILE_DEFINITIONS_DEBUG )
set(tinycbor_COMPILE_OPTIONS_C_DEBUG )
set(tinycbor_COMPILE_OPTIONS_CXX_DEBUG )
set(tinycbor_LIB_DIRS_DEBUG "${tinycbor_PACKAGE_FOLDER_DEBUG}/lib")
set(tinycbor_BIN_DIRS_DEBUG )
set(tinycbor_LIBRARY_TYPE_DEBUG UNKNOWN)
set(tinycbor_IS_HOST_WINDOWS_DEBUG 0)
set(tinycbor_LIBS_DEBUG tinycbor)
set(tinycbor_SYSTEM_LIBS_DEBUG )
set(tinycbor_FRAMEWORK_DIRS_DEBUG )
set(tinycbor_FRAMEWORKS_DEBUG )
set(tinycbor_BUILD_DIRS_DEBUG )
set(tinycbor_NO_SONAME_MODE_DEBUG FALSE)


# COMPOUND VARIABLES
set(tinycbor_COMPILE_OPTIONS_DEBUG
    "$<$<COMPILE_LANGUAGE:CXX>:${tinycbor_COMPILE_OPTIONS_CXX_DEBUG}>"
    "$<$<COMPILE_LANGUAGE:C>:${tinycbor_COMPILE_OPTIONS_C_DEBUG}>")
set(tinycbor_LINKER_FLAGS_DEBUG
    "$<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,SHARED_LIBRARY>:${tinycbor_SHARED_LINK_FLAGS_DEBUG}>"
    "$<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,MODULE_LIBRARY>:${tinycbor_SHARED_LINK_FLAGS_DEBUG}>"
    "$<$<STREQUAL:$<TARGET_PROPERTY:TYPE>,EXECUTABLE>:${tinycbor_EXE_LINK_FLAGS_DEBUG}>")


set(tinycbor_COMPONENTS_DEBUG )