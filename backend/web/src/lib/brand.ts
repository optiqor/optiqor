// Brand tokens loaded at build time from brand/tokens.json
// (Apache-2.0, single source of truth shared with the CLI's pkg/htmlrender).
// Backend frontend reads these once and exposes them as TS constants so
// callers stay decoupled from the file location.

import tokens from "../../../../brand/tokens.json";

export const brand = tokens;
export const color = tokens.color;
export const type = tokens.type;
export const radius = tokens.radius;
export const space = tokens.space;
export const motion = tokens.motion;
export const disclosure = tokens.disclosure;
