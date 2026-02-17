# Step 4 – Mouse Support

Step 3 gave us visual updates and keyboard control. Step 4 adds mouse interaction, allowing us to move the cursor and click inside the remote session.

## Objectives

1.  **Mouse Capture (Client)**
    - Capture `mousemove`, `mousedown`, and `mouseup` events on the viewer.
    - Normalize coordinates to a 0.0-1.0 range to be resolution-independent.
    - Send events via WebSocket.

2.  **Mouse Injection (Server)**
    - Receive mouse events from WebSocket.
    - Use `swaymsg` to inject mouse events into the Sway session.
        - `swaymsg seat seat0 cursor set <x> <y>` for movement.
        - `swaymsg seat seat0 cursor press/release <button>` for clicks.
    - Determine screen resolution dynamically using `swaymsg -t get_outputs` to map normalized coordinates to pixels.

3.  **Refinement**
    - Ensure the mouse position is accurate.
    - Handle basic buttons (Left, Right, Middle).

## Deliverables

- `step4/` folder (based on `step3/`) with:
    - `scripts/server.ts`: Updated to handle mouse messages and use `swaymsg`.
    - `public/viewer.html`: Updated to capture and send mouse events.
- `step4.md`: This documentation.
