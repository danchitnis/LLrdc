import ctypes
import sys
import os
import time

# Define Wayland structures
class wl_message(ctypes.Structure):
    _fields_ = [("name", ctypes.c_char_p),
                ("signature", ctypes.c_char_p),
                ("types", ctypes.c_void_p)]

class wl_interface(ctypes.Structure):
    _fields_ = [("name", ctypes.c_char_p),
                ("version", ctypes.c_int),
                ("method_count", ctypes.c_int),
                ("methods", ctypes.POINTER(wl_message)),
                ("event_count", ctypes.c_int),
                ("events", ctypes.POINTER(wl_message))]

class wl_argument(ctypes.Union):
    _fields_ = [("i", ctypes.c_int32),
                ("u", ctypes.c_uint32),
                ("f", ctypes.c_int32),
                ("s", ctypes.c_char_p),
                ("o", ctypes.c_void_p),
                ("n", ctypes.c_uint32),
                ("a", ctypes.c_void_p),
                ("h", ctypes.c_int32)]

# Load libwayland-client
lib = ctypes.cdll.LoadLibrary("libwayland-client.so.0")

lib.wl_display_connect.restype = ctypes.c_void_p
lib.wl_display_connect.argtypes = [ctypes.c_char_p]
lib.wl_proxy_marshal_array_constructor.restype = ctypes.c_void_p
lib.wl_proxy_marshal_array_constructor.argtypes = [ctypes.c_void_p, ctypes.c_uint32, ctypes.POINTER(wl_argument), ctypes.POINTER(wl_interface)]
lib.wl_proxy_add_listener.argtypes = [ctypes.c_void_p, ctypes.c_void_p, ctypes.c_void_p]
lib.wl_display_roundtrip.argtypes = [ctypes.c_void_p]

# Get exported interfaces
wl_registry_interface = wl_interface.in_dll(lib, "wl_registry_interface")

# Define our interfaces
manager_methods = (wl_message * 1)(
    wl_message(b"create_virtual_pointer", b"?on", None)
)
manager_interface = wl_interface(
    b"zwlr_virtual_pointer_manager_v1", 1, 1, manager_methods, 0, None
)

pointer_methods = (wl_message * 5)(
    wl_message(b"motion", b"uif", None),
    wl_message(b"motion_absolute", b"uuuuu", None),
    wl_message(b"button", b"uuu", None),
    wl_message(b"axis", b"uuf", None),
    wl_message(b"frame", b"", None)
)
pointer_interface = wl_interface(
    b"zwlr_virtual_pointer_v1", 1, 5, pointer_methods, 0, None
)

# Global variables
manager_proxy = None
pointer_proxy = None

# Registry listener
@ctypes.CFUNCTYPE(None, ctypes.c_void_p, ctypes.c_void_p, ctypes.c_uint32, ctypes.c_char_p, ctypes.c_uint32)
def registry_handle_global(data, registry, name, interface, version):
    global manager_proxy
    if interface == b"zwlr_virtual_pointer_manager_v1":
        args = (wl_argument * 4)()
        args[0].u = name
        args[1].s = manager_interface.name
        args[2].u = 1
        args[3].n = 0
        manager_proxy = lib.wl_proxy_marshal_array_constructor(
            registry, 0, args, ctypes.byref(manager_interface)
        )

@ctypes.CFUNCTYPE(None, ctypes.c_void_p, ctypes.c_void_p, ctypes.c_uint32)
def registry_handle_global_remove(data, registry, name):
    pass

class wl_registry_listener(ctypes.Structure):
    _fields_ = [("global", ctypes.c_void_p),
                ("global_remove", ctypes.c_void_p)]

listener = wl_registry_listener(
    ctypes.cast(registry_handle_global, ctypes.c_void_p),
    ctypes.cast(registry_handle_global_remove, ctypes.c_void_p)
)

# Main
display = lib.wl_display_connect(None)
if not display:
    sys.exit(1)

args = (wl_argument * 1)()
args[0].n = 0
registry = lib.wl_proxy_marshal_array_constructor(display, 1, args, ctypes.byref(wl_registry_interface))
lib.wl_proxy_add_listener(registry, ctypes.byref(listener), None)
lib.wl_display_roundtrip(display)

if not manager_proxy:
    sys.exit(1)

args = (wl_argument * 2)()
args[0].o = None
args[1].n = 0
pointer_proxy = lib.wl_proxy_marshal_array_constructor(
    manager_proxy, 0, args, ctypes.byref(pointer_interface)
)
lib.wl_display_roundtrip(display)
print("Virtual pointer created", file=sys.stderr)

for line in sys.stdin:
    parts = line.split()
    if not parts: continue
    cmd = parts[0]
    t = int(time.time() * 1000) & 0xFFFFFFFF
    if cmd == 'm':
        x, y, w, h = map(int, parts[1:])
        args = (wl_argument * 5)()
        args[0].u = t
        args[1].u = x
        args[2].u = y
        args[3].u = w
        args[4].u = h
        lib.wl_proxy_marshal_array_constructor(pointer_proxy, 1, args, None)
        lib.wl_proxy_marshal_array_constructor(pointer_proxy, 4, None, None)
    elif cmd == 'b':
        btn, state = map(int, parts[1:])
        code = 0x110
        if btn == 1: code = 0x112
        if btn == 2: code = 0x111
        args = (wl_argument * 3)()
        args[0].u = t
        args[1].u = code
        args[2].u = state
        lib.wl_proxy_marshal_array_constructor(pointer_proxy, 2, args, None)
        lib.wl_proxy_marshal_array_constructor(pointer_proxy, 4, None, None)
    lib.wl_display_roundtrip(display)
