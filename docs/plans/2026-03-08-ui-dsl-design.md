# UI DSL Design

**Date:** 2026-03-08
**Status:** Approved

## Overview

A JSON-based DSL for describing user interfaces that can be rendered to both shadcn/React (web) and SwiftUI (iOS) via runtime interpreters. Designed for LLM generation — the DSL is simple enough for an LLM to produce directly as structured text.

### Goals

- **Cross-platform:** Single DSL renders to shadcn (web) and SwiftUI (iOS)
- **LLM-friendly:** Small vocabulary, JSON format, schema-validatable, fits in a system prompt
- **Scalable:** From simple widgets to full app screens
- **Extensible:** Host apps can register custom components without changing the spec
- **Standalone first:** Independent library, with future Rubichan integration

### Non-Goals

- Built-in state management (host app manages state)
- CSS/styling escape hatches (renderers handle theming)
- Code generation (runtime interpretation only)

## 1. Node Structure

Every element in the DSL is a **Node** — a uniform JSON object:

```json
{
  "type": "ComponentName",
  "props": { },
  "events": { },
  "children": [ ]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | `string` | Yes | Component name from the DSL vocabulary |
| `props` | `object` | No | Key-value properties specific to the component type |
| `events` | `object` | No | Maps event names (`onClick`, `onChange`, `onSubmit`) to action identifier strings |
| `children` | `string \| Node \| Node[]` | No | Text content or nested nodes |

### Design Decisions

- **`children` is polymorphic** — a string for leaf text, or nested nodes. Keeps LLM output compact.
- **`events` are separate from `props`** — renderers can distinguish interactive bindings from visual configuration. React treats events as props; SwiftUI uses modifiers. Separation lets each renderer map idiomatically.
- **Action identifiers are opaque strings** — the host app registers handlers by name. The DSL declares intent, not behavior.

## 2. Component Vocabulary

Two layers: **Layout Primitives** (platform-agnostic) and **UI Components** (mapped to shadcn/SwiftUI).

### Layout Primitives

| DSL Type | Web (React) | SwiftUI | Key Props |
|----------|-------------|---------|-----------|
| `Page` | `<div>` with page layout | `NavigationStack` | `title`, `padding` |
| `Stack` | Flex container | `VStack` / `HStack` | `direction` (`vertical`\|`horizontal`), `gap`, `align`, `justify` |
| `Grid` | CSS Grid | `LazyVGrid` / `LazyHGrid` | `columns`, `gap` |
| `ScrollArea` | shadcn `ScrollArea` | `ScrollView` | `direction` |
| `Separator` | shadcn `Separator` | `Divider` | `orientation` |
| `Spacer` | flex spacer `<div>` | `Spacer` | — |

### UI Components

| DSL Type | Web (shadcn) | SwiftUI | Key Props |
|----------|-------------|---------|-----------|
| `Text` | `<p>` / `<span>` | `Text` | `variant` (`body`\|`muted`\|`lead`\|`code`), `weight` |
| `Heading` | `<h1>`–`<h6>` | `Text` + font style | `level` (1–6) |
| `Button` | `Button` | `Button` | `variant` (`default`\|`secondary`\|`outline`\|`ghost`\|`destructive`\|`link`), `size` (`sm`\|`default`\|`lg`\|`icon`) |
| `Input` | `Input` | `TextField` | `type`, `placeholder`, `label`, `disabled` |
| `Textarea` | `Textarea` | `TextEditor` | `placeholder`, `rows` |
| `Checkbox` | `Checkbox` | `Toggle` (checkbox style) | `label`, `checked` |
| `Switch` | `Switch` | `Toggle` | `label`, `checked` |
| `Select` | `Select` | `Picker` | `label`, `placeholder`, `options: [{value, label}]` |
| `RadioGroup` | `RadioGroup` | `Picker` (segmented) | `label`, `options: [{value, label}]` |
| `Slider` | `Slider` | `Slider` | `min`, `max`, `step`, `value` |
| `Badge` | `Badge` | Styled `Text` | `variant` (`default`\|`secondary`\|`outline`\|`destructive`) |
| `Avatar` | `Avatar` | `AsyncImage` + clip | `src`, `fallback` |
| `Image` | `<img>` | `Image` / `AsyncImage` | `src`, `alt`, `fit` (`cover`\|`contain`\|`fill`) |
| `Progress` | `Progress` | `ProgressView` | `value` (0–100) |

### Composite Components

| DSL Type | Web (shadcn) | SwiftUI | Key Props |
|----------|-------------|---------|-----------|
| `Card` | `Card` | Styled `VStack` + `RoundedRectangle` | — |
| `CardHeader` | `CardHeader` | Layout container | — |
| `CardTitle` | `CardTitle` | Styled `Text` | — |
| `CardDescription` | `CardDescription` | Styled `Text` | — |
| `CardContent` | `CardContent` | Layout container | — |
| `CardFooter` | `CardFooter` | Layout container | — |
| `Dialog` | `Dialog` | `.sheet` modifier | `title`, `open` |
| `Alert` | `Alert` | `.alert` modifier | `title`, `variant` (`default`\|`destructive`) |
| `Tabs` | `Tabs` | `TabView` | `defaultValue` |
| `TabItem` | `TabsTrigger` + `TabsContent` | `.tabItem` | `value`, `label` |
| `Table` | `Table` | `List` / `Grid` | `columns: [{key, header, width?}]`, `rows: [{}]` |
| `Form` | `<form>` | `Form` | — |
| `FormField` | Label + control wrapper | `Section` row | `label`, `description`, `required` |

## 3. Schema & Validation

### Document Structure

```json
{
  "$schema": "https://rubichan.dev/ui-dsl/v1.schema.json",
  "version": "1.0",
  "actions": {
    "saveProfile": { "description": "Save the user profile form" },
    "cancel": { "description": "Discard changes and go back" }
  },
  "root": { "type": "Page", "props": {}, "children": [] }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `$schema` | `string` | No | Schema URL for editor validation |
| `version` | `string` | Yes | DSL version for forward compatibility |
| `actions` | `object` | No | Declares available action handlers with descriptions and optional params |
| `root` | `Node` | Yes | The root node of the UI tree |

### Type-Specific Prop Validation

Each component type has its own prop schema. Unknown props are caught by the validator. For example, `Button` only accepts `variant`, `size`, and `disabled`.

### Validation Levels

| Level | Behavior | Use Case |
|-------|----------|----------|
| `strict` | Reject unknown types, unknown props, unreferenced actions | Production |
| `warn` | Accept but log warnings for unknowns | Development/preview |
| `lenient` | Accept anything, skip unknowns silently | LLM prototyping |

`lenient` mode is critical for LLM workflows — render what you can, skip what you don't understand, so the output is usable even with minor hallucinations.

## 4. Renderer Architecture

### Renderer Contract

Both platforms implement the same conceptual interface:

```
Renderer
  ├── registerComponent(type, renderFn)   // register how to render a DSL type
  ├── registerAction(name, handler)       // register event handler by name
  ├── render(dslDocument) → NativeView    // walk tree, produce UI
  └── validate(dslDocument) → errors[]    // check before rendering
```

### Web Renderer (React + shadcn)

A React component that recursively renders the DSL tree:

```jsx
<DSLRenderer document={dsl} actions={actionHandlers} validationLevel="warn" />
```

Component registry maps DSL types to React components. Props are spread onto components. Events are wired to the action handlers map. Unknown types render a placeholder in `warn` mode.

### SwiftUI Renderer (iOS)

A Swift package providing:

```swift
DSLView(document: dslDocument, actions: actionHandlers)
```

Registry maps DSL types to `@ViewBuilder` functions. `Stack` checks `direction` prop to emit `VStack` or `HStack`. `Card` composes `VStack` + background + shadow.

### Custom Component Registration

Host apps extend the vocabulary without changing the spec:

```javascript
// Web
renderer.register("CustomChart", (props, children) => <MyChart {...props} />)
```

```swift
// Swift
renderer.register("CustomChart") { props, children in
    MyChartView(data: props["data"])
}
```

### Rendering Pipeline

```
DSL JSON → Parse & Validate → Walk Node Tree (depth-first)
  ├─ Lookup type in registry
  │    ├─ Found → render with props, wire events, recurse into children
  │    └─ Not found → placeholder / skip / error (per validation level)
  └─ → Native View Tree (React DOM / SwiftUI View)
```

## 5. LLM Integration

### Compact Reference Card (~600 tokens)

Designed to fit in an LLM system prompt:

```
UI DSL Reference:
- Layout: Page(title), Stack(direction:v|h, gap, align, justify), Grid(columns, gap), ScrollArea(direction), Separator, Spacer
- Text: Text(variant:body|muted|lead|code), Heading(level:1-6)
- Input: Input(type,placeholder,label), Textarea(placeholder,rows), Checkbox(label,checked), Switch(label,checked), Select(label,options:[{value,label}]), RadioGroup(label,options), Slider(min,max,step,value)
- Display: Badge(variant), Avatar(src,fallback), Image(src,alt,fit), Progress(value:0-100)
- Composite: Card>CardHeader>CardTitle+CardDescription, CardContent, CardFooter, Dialog(title,open), Alert(title,variant), Tabs(defaultValue)>TabItem(value,label), Table(columns,rows), Form>FormField(label,description,required)
- Events: onClick, onChange, onSubmit → action name string

Node format: { "type": "...", "props": {}, "events": {}, "children": "..." | [...] }
```

### Generation Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| **Full document** | LLM outputs entire `{ version, root, actions }` | Generating a complete screen |
| **Partial subtree** | LLM outputs a single `Node` | Modifying or adding to existing UI |

### Validation Feedback Loop

```
LLM generates DSL → Validator finds errors → Errors fed back to LLM → LLM self-corrects
```

Example: LLM outputs `"type": "PasswordInput"` → Validator: "Unknown type 'PasswordInput'. Did you mean 'Input' with props.type='password'?" → LLM corrects.

### Prompt-Driven Customization

Host apps constrain LLM output by narrowing the reference card:

- Restrict available types: "Only use Card, Text, Button, Badge"
- Declare available actions: "Available actions: addToCart, removeItem"
- Exclude types: "Never use Dialog or Alert"

## Example: Settings Form

```json
{
  "version": "1.0",
  "actions": {
    "saveProfile": { "description": "Save profile changes" },
    "cancel": { "description": "Discard changes" },
    "updateName": { "description": "Update name field", "params": ["value"] },
    "toggleNotifications": { "description": "Toggle notifications", "params": ["checked"] }
  },
  "root": {
    "type": "Page",
    "props": { "title": "Settings" },
    "children": [
      {
        "type": "Card",
        "children": [
          { "type": "CardHeader", "children": [
            { "type": "CardTitle", "children": "Profile" },
            { "type": "CardDescription", "children": "Update your personal information." }
          ]},
          { "type": "CardContent", "children": [
            { "type": "Form", "events": { "onSubmit": "saveProfile" }, "children": [
              { "type": "FormField", "props": { "label": "Name", "required": true }, "children": [
                { "type": "Input", "props": { "placeholder": "John Doe" }, "events": { "onChange": "updateName" } }
              ]},
              { "type": "FormField", "props": { "label": "Bio" }, "children": [
                { "type": "Textarea", "props": { "placeholder": "Tell us about yourself", "rows": 3 } }
              ]},
              { "type": "FormField", "props": { "label": "Notifications" }, "children": [
                { "type": "Switch", "props": { "label": "Email notifications", "checked": true }, "events": { "onChange": "toggleNotifications" } }
              ]}
            ]}
          ]},
          { "type": "CardFooter", "children": [
            { "type": "Button", "props": { "variant": "outline" }, "children": "Cancel", "events": { "onClick": "cancel" } },
            { "type": "Button", "children": "Save", "events": { "onClick": "saveProfile" } }
          ]}
        ]
      }
    ]
  }
}
```
