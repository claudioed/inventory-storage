import { Routes, Route } from "react-router-dom";
import { InventoryScreen } from "./screens/InventoryScreen";

/** Exposed as inventory_mfe/App via Module Federation. Routed under
 *  /inventory/* by the shell -- uses relative routes so this component
 *  works identically mounted under a prefix (in the shell) or at /
 *  (standalone dev, see main.tsx). Single default route to
 *  InventoryScreen -- this repo doesn't need sub-routes yet. */
export default function App() {
  return (
    <Routes>
      <Route path="/" element={<InventoryScreen />} />
    </Routes>
  );
}
