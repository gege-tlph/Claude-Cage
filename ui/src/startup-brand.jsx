import React from "react";
import { CageLogo } from "./cage-logo.jsx";
import { LanguageSettings } from "./language-settings";
import { ProductName } from "./product-name.jsx";
import "./startup-brand.css";

// One header for the account page and first-run setup, so moving from one to
// the other keeps the logo and the language menu in place.
export function StartupBrand({ label }) {
  return <header className="startup-brand">
    <CageLogo /><strong><ProductName /></strong>{label && <span>{label}</span>}
    <div className="startup-language-slot"><LanguageSettings compact /></div>
  </header>;
}
