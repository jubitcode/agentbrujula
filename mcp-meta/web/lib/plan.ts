/**
 * The plan: what the PM produces and the screen renders.
 *
 * Adapted from CodeZard's `lib/plan/schema.ts`, with the shape changed from a
 * software specification (entities, roles, flows) to a project plan. The
 * software fields survive as optional, because a tool-sharing app has entities
 * and a hiring process does not, and forcing them produces a worse plan.
 *
 * Every field here is a decision, and the ones worth stating:
 */

export type Milestone = {
  name: string
  /** Something a person could look at and confirm happened. Not "Development". */
  outcome: string
}

export type Task = {
  id: string
  title: string
  milestone: string
  /**
   * Task ids this one waits for.
   *
   * Dependencies rather than a numbered list: two tasks with nothing between
   * them is a statement that they can start at the same time, and a flat
   * ordering hides that.
   */
  dependsOn: string[]
}

export type Risk = { statement: string; mitigation: string }

export type Scope = {
  included: string[]
  /**
   * What a reader would reasonably assume is included and is not.
   *
   * The half that does the work. An alcance that only lists what it covers
   * bounds nothing, and this is the field the PM fills when it decides
   * something is out — which is how a rejection becomes visible later instead
   * of looking like an omission.
   */
  excluded: string[]
}

export type Entity = { name: string; description: string }
export type Flow = { name: string; steps: string[] }
export type Constraint = {
  kind: "performance" | "security" | "compatibility" | "other"
  statement: string
}

export type Plan = {
  /** 1, 2, 3… A rejection never mutates a plan: it produces the next version. */
  version: number
  objective: string
  scope: Scope
  milestones: Milestone[]
  tasks: Task[]
  risks: Risk[]
  /**
   * What the PM could not settle, in plain words.
   *
   * Always present and never collapsed on screen. A plan that does not say
   * what it does not know reads as more certain than it is, and the pod
   * substitutes a sentence saying so when the model returns none.
   */
  openQuestions: string[]
  doneCriteria: string[]

  /** Only when the project is a software system. Absent otherwise. */
  entities?: Entity[]
  flows?: Flow[]
  constraints?: Constraint[]

  status: "draft" | "approved"
  /** Why this version exists. Absent for v1; the user's own words after that. */
  revisionOf?: { version: number; feedback: string }
}

/** A file the user offered that did not make it into the prompt, and why. */
export type SkippedFile = { name: string; reason: string }

export type PlanResponse = {
  plan?: Plan
  skipped_files?: SkippedFile[]
  /** The pod's own message. Tool failures arrive as HTTP 200 with this set. */
  error?: string
}
