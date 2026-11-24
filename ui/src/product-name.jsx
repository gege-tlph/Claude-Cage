import React from "react";
import "./product-name.css";

// The product name with its edition in a small framed tag: Cage [lite].
// The space stays in the text, so copying and screen readers get "Cage lite".
export function ProductName() {
  return <span className="product-name">Cage <span className="product-edition">lite</span></span>;
}
