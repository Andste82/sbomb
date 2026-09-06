from conan import ConanFile
from conan.tools.cmake import CMake, CMakeToolchain, cmake_layout
from conan.tools.files import copy


class TinycborConan(ConanFile):
    """A fixture dependency, created into a local Conan cache by regen.sh."""

    name = "tinycbor"
    version = "0.6.1"
    license = "MIT"
    author = "Tinycbor Authors"
    homepage = "https://example.invalid/tinycbor"
    description = "A fixture dependency installed by Conan."
    settings = "os", "compiler", "build_type", "arch"
    exports_sources = "CMakeLists.txt", "*.c", "*.h", "LICENSE"
    generators = "CMakeToolchain"

    def layout(self):
        cmake_layout(self)

    def build(self):
        cmake = CMake(self)
        cmake.configure()
        cmake.build()

    def package(self):
        copy(self, "*.h", self.source_folder, f"{self.package_folder}/include")
        copy(self, "LICENSE", self.source_folder, f"{self.package_folder}/licenses")
        copy(self, "*.a", self.build_folder, f"{self.package_folder}/lib", keep_path=False)

    def package_info(self):
        self.cpp_info.libs = ["tinycbor"]
