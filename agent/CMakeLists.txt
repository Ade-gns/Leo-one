cmake_minimum_required(VERSION 3.20)
project(leo-agent VERSION 1.0.0 LANGUAGES C)

# ─── Standard C11 ──────────────────────────────────────────────────────────
set(CMAKE_C_STANDARD 11)
set(CMAKE_C_STANDARD_REQUIRED ON)
set(CMAKE_C_EXTENSIONS OFF)

# ─── Options de build ────────────────────────────────────────────────────────
option(TARGET_OS "Système cible (linux|windows|macos)" "linux")

# Flags de sécurité et optimisation communs
add_compile_options(
    -Wall -Wextra -Wpedantic
    -Wformat=2 -Wformat-security
    -fstack-protector-strong
    -D_FORTIFY_SOURCE=2
    $<$<CONFIG:Release>:-O2>
    $<$<CONFIG:Debug>:-O0 -g -fsanitize=address,undefined>
)
add_link_options(
    $<$<CONFIG:Debug>:-fsanitize=address,undefined>
    -Wl,-z,relro -Wl,-z,now   # RELRO complet (Linux uniquement)
)

# ─── Sources communes ─────────────────────────────────────────────────────────
set(AGENT_COMMON_SOURCES
    src/main.c
    src/agent.c
    src/config.c
    src/logger.c
    src/protocol.c
    src/connection.c
    src/metrics.c
    third_party/cjson/cJSON.c
)

# ─── Sources spécifiques à la plateforme ─────────────────────────────────────
if(TARGET_OS STREQUAL "linux" OR CMAKE_SYSTEM_NAME STREQUAL "Linux")
    list(APPEND AGENT_COMMON_SOURCES
        platform/linux/metrics_linux.c
        platform/linux/service_linux.c
        platform/linux/executor_linux.c
        platform/linux/crypto_linux.c
    )
    add_definitions(-DLEO_PLATFORM_LINUX)

elseif(TARGET_OS STREQUAL "windows" OR CMAKE_SYSTEM_NAME STREQUAL "Windows")
    list(APPEND AGENT_COMMON_SOURCES
        platform/windows/metrics_win.c
        platform/windows/service_win.c
        platform/windows/executor_win.c
        platform/windows/crypto_win.c
    )
    add_definitions(-DLEO_PLATFORM_WINDOWS)

elseif(TARGET_OS STREQUAL "macos" OR CMAKE_SYSTEM_NAME STREQUAL "Darwin")
    list(APPEND AGENT_COMMON_SOURCES
        platform/macos/metrics_macos.c
        platform/macos/service_macos.c
        platform/macos/executor_macos.c
        platform/macos/crypto_macos.c
    )
    add_definitions(-DLEO_PLATFORM_MACOS)
endif()

# ─── Exécutable ───────────────────────────────────────────────────────────────
add_executable(leo-agent ${AGENT_COMMON_SOURCES})

target_include_directories(leo-agent PRIVATE
    include
    src
    third_party/cjson
    third_party/libwebsockets/include
)

# ─── Dépendances externes ─────────────────────────────────────────────────────

# libwebsockets (build statique vendored)
add_subdirectory(third_party/libwebsockets EXCLUDE_FROM_ALL)
target_link_libraries(leo-agent PRIVATE websockets_static)

# Threads POSIX
find_package(Threads REQUIRED)
target_link_libraries(leo-agent PRIVATE Threads::Threads)

# OpenSSL (nécessaire pour libwebsockets)
find_package(OpenSSL REQUIRED)
target_link_libraries(leo-agent PRIVATE OpenSSL::SSL OpenSSL::Crypto)

# ─── Informations de version compilées dans le binaire ───────────────────────
target_compile_definitions(leo-agent PRIVATE
    LEO_BUILD_VERSION="${CMAKE_PROJECT_VERSION}"
)

# ─── Installation ─────────────────────────────────────────────────────────────
install(TARGETS leo-agent
    RUNTIME DESTINATION /opt/leo-one/agent
)

# ─── Strip du binaire en Release (réduit la surface d'analyse) ───────────────
if(CMAKE_BUILD_TYPE STREQUAL "Release")
    add_custom_command(TARGET leo-agent POST_BUILD
        COMMAND ${CMAKE_STRIP} --strip-all $<TARGET_FILE:leo-agent>
        COMMENT "Strip du binaire de release"
    )
endif()
