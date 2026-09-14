// PR-5e / L-C7: the turn gate's second half — a session bound to a model the
// current catalogue no longer offers.
//
// These live beside catalog.test.ts rather than in it because the subject is 闭环
// L-C7 ("会话绑定的模型被下架之后，用户看到什么、还能做什么"), not L-C2 ("目录不可用").
// The two are asked of the same two functions, and the counter-examples below are
// what keeps them apart: a catalogue-state sentence must not be shadowed by the new
// one, and the new one must not be invented for a binding it cannot attribute.
import { describe, it, expect, beforeEach } from "vitest";
import { get } from "svelte/store";
import { chatModes, canStartTurn, catalogNoticeKey, sessionModelWithdrawn } from "./chatMode";
import { catalogState } from "./product";
import { chatModel, sessions } from "./stores";
import type { ChatModeDTO } from "./api";
import type { Session } from "./types";

const GATEWAY = "buding-gateway::";

// projectCatalog builds compositeId as <endpoint id>::<catalog id>, and the
// endpoint id has one owner (internal/productprofile). A test may state it: it is
// standing in for the server's data, which is what the projection receives over
// the wire — the production code reads the prefix out of that data and holds no
// constant of its own.
function projection(rows: Array<{ id: string; compositeId?: string }>): ChatModeDTO[] {
  return [
    {
      id: "smart",
      models: rows.map((r) => ({ id: r.id, compositeId: r.compositeId ?? GATEWAY + r.id, displayName: { zh: r.id, en: r.id } })),
    } as ChatModeDTO,
  ];
}

function bind(sid: string, ref: string) {
  chatModel.update((m) => ({ ...m, [sid]: ref }));
}

function session(sid: string, modelId: string): Session {
  return { id: sid, model_id: modelId } as Session;
}

beforeEach(() => {
  catalogState.set("ready");
  chatModes.set([]);
  chatModel.set({});
  sessions.set([]);
});

describe("a session whose model the catalogue dropped", () => {
  it("is refused, and the sentence names the model — not the catalogue", () => {
    chatModes.set(projection([{ id: "buding-cloud-fast" }]));
    bind("s1", GATEWAY + "buding-cloud-retired");

    expect(sessionModelWithdrawn("s1")).toBe(true);
    expect(canStartTurn("s1")).toBe(false);
    expect(catalogNoticeKey("s1")).toBe("session.model_withdrawn");
  });

  it("is judged when the binding comes from the sessions list", () => {
    // A relaunched window has no chatModel entry until the session is opened; the
    // sessions list is where the binding already is, and `model_id` is the
    // server's own key for it (api.ts updateSessionModel).
    chatModes.set(projection([{ id: "buding-cloud-fast" }]));
    sessions.set([session("s1", GATEWAY + "buding-cloud-retired")]);

    expect(sessionModelWithdrawn("s1")).toBe(true);
  });

  it("is reported by whichever value IS in the projection, composite or bare", () => {
    // ModeMenu.isActive treats the two as naming the same model; the check has to
    // agree, or a session would read as withdrawn while the picker highlights it.
    chatModes.set(projection([{ id: "buding-cloud-pro" }]));

    bind("composite", GATEWAY + "buding-cloud-pro");
    expect(sessionModelWithdrawn("composite")).toBe(false);

    bind("bare", "buding-cloud-pro");
    // Bare ids are not attributable (see the next describe), so this is NOT the
    // "no longer offered" case — it is simply not this half's business.
    expect(sessionModelWithdrawn("bare")).toBe(false);

    // The comparison itself, isolated: a binding that matches a row's BARE id
    // while carrying the prefix is found.
    bind("prefixed-bare", GATEWAY + "buding-cloud-pro");
    expect(sessionModelWithdrawn("prefixed-bare")).toBe(false);
  });

  it("is not refused while the catalogue still lists it", () => {
    chatModes.set(projection([{ id: "buding-cloud-pro" }]));
    bind("s1", GATEWAY + "buding-cloud-pro");

    expect(sessionModelWithdrawn("s1")).toBe(false);
    expect(canStartTurn("s1")).toBe(true);
  });

  it("is not refused when a model moved to another mode group", () => {
    // Membership anywhere in the projection counts: 下架 means gone from the
    // catalogue, not "no longer in this conversation's group".
    chatModes.set([
      { id: "smart", models: [] } as unknown as ChatModeDTO,
      projection([{ id: "buding-cloud-pro" }])[0],
    ]);
    bind("s1", GATEWAY + "buding-cloud-pro");

    expect(sessionModelWithdrawn("s1")).toBe(false);
  });
});

describe("what the withdrawal check must NOT claim", () => {
  it("says nothing about a conversation with no model bound", () => {
    chatModes.set(projection([{ id: "buding-cloud-fast" }]));

    expect(sessionModelWithdrawn("fresh")).toBe(false);
    expect(canStartTurn("fresh")).toBe(true);
  });

  it("says nothing when no session id was given at all", () => {
    // ModeMenu's empty-list rendering calls these with no session.
    chatModes.set(projection([{ id: "buding-cloud-fast" }]));

    expect(sessionModelWithdrawn(undefined)).toBe(false);
    expect(sessionModelWithdrawn(null)).toBe(false);
  });

  it("says nothing when the projection is empty — B9 owns that sentence", () => {
    // Nothing is listed, so there is nothing to compare against, and
    // "this group has no models" is the truth. Claiming a withdrawal would
    // invent a fact about a model nobody can see.
    chatModes.set([]);
    bind("s1", GATEWAY + "buding-cloud-retired");

    expect(sessionModelWithdrawn("s1")).toBe(false);
    expect(catalogNoticeKey("s1")).toBe("mode.no_models");
  });

  it("says nothing about a model that never came from the catalogue", () => {
    // A data/config.yml endpoint (D-008) or a CLI session on the same data root:
    // its model is not in the picker's list and never was. Calling it "withdrawn"
    // would be a fabrication about the user's session, and the server's own guard
    // reads the same distinction (gatewayBound) as the backstop.
    chatModes.set(projection([{ id: "buding-cloud-fast" }]));
    bind("s1", "ep-deepseek::deepseek-v4-pro");

    expect(sessionModelWithdrawn("s1")).toBe(false);
    expect(canStartTurn("s1")).toBe(true);
  });

  it("lets the catalogue-state family speak first", () => {
    // Order matters and is load-bearing: "we have no usable list" is the more
    // fundamental fact, and it is also the reason a model cannot be found. A
    // withdrawal is only ever reported about a catalogue that is in hand.
    chatModes.set(projection([{ id: "buding-cloud-fast" }]));
    bind("s1", GATEWAY + "buding-cloud-retired");

    for (const [state, key] of [
      ["absent", "catalog.absent"],
      ["stale", "catalog.stale"],
      ["unverifiable", "catalog.unverifiable"],
    ] as const) {
      catalogState.set(state);
      expect(catalogNoticeKey("s1")).toBe(key);
      expect(canStartTurn("s1")).toBe(false);
    }
  });
});

describe("the gate reads and never writes", () => {
  // "不得静默换模型" (B8) at this seam means: asking the gate must not re-point the
  // session at something else. The only writers of a binding are user gestures
  // (Composer's pickModeModel, mobile NewTask) — nothing auto-rebinds — so the
  // property to pin is that this check joins neither list.
  it("leaves the binding naming the withdrawn model, not the mode's default", () => {
    chatModes.set([
      {
        id: "smart",
        // The group still HAS a model — and a default — so "the session was
        // re-pointed at the mode's default" is a reachable mistake rather than a
        // hypothetical one.
        models: [
          { id: "buding-cloud-pro", compositeId: GATEWAY + "buding-cloud-pro", displayName: { zh: "新", en: "new" } },
        ],
        defaultModel: GATEWAY + "buding-cloud-pro",
      } as unknown as ChatModeDTO,
    ]);
    bind("s1", GATEWAY + "buding-cloud-retired");

    expect(canStartTurn("s1")).toBe(false);
    expect(catalogNoticeKey("s1")).toBe("session.model_withdrawn");

    // A silent switch would show up as the default here — the one thing the
    // requirement names outright.
    expect(get(chatModel).s1).toBe(GATEWAY + "buding-cloud-retired");
    expect(get(chatModel).s1).not.toBe(GATEWAY + "buding-cloud-pro");
  });

  it("changes nothing it read", () => {
    chatModes.set(projection([{ id: "buding-cloud-fast" }]));
    bind("s1", GATEWAY + "buding-cloud-retired");
    sessions.set([session("s1", GATEWAY + "buding-cloud-retired")]);

    // Deep copies, so a mutation of any store is visible rather than compared by
    // reference.
    const before = {
      models: JSON.parse(JSON.stringify(get(chatModel))),
      sessions: JSON.parse(JSON.stringify(get(sessions))),
      modes: JSON.parse(JSON.stringify(get(chatModes))),
      catalog: get(catalogState),
    };

    sessionModelWithdrawn("s1");
    canStartTurn("s1");
    catalogNoticeKey("s1");

    expect(get(chatModel)).toEqual(before.models);
    expect(get(sessions)).toEqual(before.sessions);
    expect(get(chatModes)).toEqual(before.modes);
    expect(get(catalogState)).toBe(before.catalog);
  });
});

describe("the gate as the composer asks it", () => {
  it("refuses a turn with an unusable catalogue even with no session yet", () => {
    // L-C2's half, unchanged by PR-5e: the first message of a new conversation
    // has no session id and is still gated on the catalogue.
    catalogState.set("stale");
    expect(canStartTurn()).toBe(false);
    expect(canStartTurn(undefined)).toBe(false);
  });

  it("is importable from chatMode, and no longer from product", async () => {
    // The move is deliberate (PR-5e): "may a turn start" and "why not" are one
    // judgement, and they were split across two modules that would now have to
    // import each other. A second exported canStartTurn elsewhere would be a
    // second answer to the same question.
    const product = await import("./product");
    expect((product as Record<string, unknown>).canStartTurn).toBeUndefined();
    expect(typeof get(catalogState)).toBe("string");
  });
});
