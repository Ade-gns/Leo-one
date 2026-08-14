/* Shim de casse pour la cross-compilation mingw-w64 depuis un système de
 * fichiers sensible à la casse (Linux). mingw-w64 fournit "psapi.h" en
 * minuscules ; du code vendored (libwebsockets/plat/windows) l'inclut comme
 * "Psapi.h" (casse Windows historique), ce qui ne pose problème que sous
 * cross-compilation — Windows et macOS ont un système de fichiers insensible
 * à la casse par défaut. Ce répertoire est ajouté en tête de l'include path
 * uniquement pour TARGET_OS=windows (voir CMakeLists.txt).
 */
#include <psapi.h>
