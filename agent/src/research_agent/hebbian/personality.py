from __future__ import annotations

from dataclasses import dataclass, field

from .introspection import AgentSelfModel, DomainProfile


@dataclass
class VoiceParameters:
    """Parameters that influence how the agent communicates."""

    hedging_level: float = 0.5  # 0 = assertive, 1 = cautious
    detail_preference: float = 0.5  # 0 = concise, 1 = detailed
    formality: float = 0.5  # 0 = casual, 1 = academic
    enthusiasm_domains: list[str] = field(default_factory=list)


@dataclass
class PersonalitySnapshot:
    """The agent's personality at a point in time."""

    description: str = ""
    strengths: list[str] = field(default_factory=list)
    growth_areas: list[str] = field(default_factory=list)
    blind_spots: list[str] = field(default_factory=list)
    voice: VoiceParameters = field(default_factory=VoiceParameters)
    reasoning_style: str = ""
    confidence_statement: str = ""


class PersonalitySynthesizer:
    """Synthesizes personality descriptions from introspection metrics."""

    def synthesize(self, model: AgentSelfModel) -> PersonalitySnapshot:
        """Generate a personality snapshot from the agent's self-model."""
        description_parts = []
        strengths = []
        growth_areas = []
        blind_spots = []

        if model.top_domains:
            top_profiles = [
                model.profiles[d] for d in model.top_domains if d in model.profiles
            ]
            if top_profiles:
                best = top_profiles[0]
                description_parts.append(
                    self._domain_identity(best, len(model.profiles))
                )
                strengths = [
                    f"{p.domain} ({p.node_count} nodes, {p.edge_count + p.cross_domain_edges} learned connections)"
                    for p in top_profiles
                ]

        if model.weakest_domains:
            weak_profiles = [
                model.profiles[d] for d in model.weakest_domains if d in model.profiles
            ]
            growth_areas = [
                f"{p.domain} (sparse connections, {p.edge_count} edges)"
                for p in weak_profiles
                if p.edge_count + p.cross_domain_edges < 5
            ]

        all_methodologies: dict[str, int] = {}
        for p in model.profiles.values():
            for m in p.active_methodologies:
                all_methodologies[m] = all_methodologies.get(m, 0) + 1

        if all_methodologies:
            top_methods = sorted(
                all_methodologies, key=all_methodologies.get, reverse=True
            )[:3]
            method_desc = self._methodology_identity(top_methods)
            description_parts.append(method_desc)

        t = model.tendencies
        reasoning_style = self._reasoning_style(
            t.depth_vs_breadth, t.theoretical_vs_practical
        )
        description_parts.append(reasoning_style)

        confidence_statement = self._confidence_statement(t.confidence_calibration)
        description_parts.append(confidence_statement)

        if model.frontier.weak_bridges:
            bridge = model.frontier.weak_bridges[0]
            description_parts.append(
                f"I sense a gap between my {bridge[0]} and {bridge[1]} knowledge — "
                f"connecting these domains would strengthen my understanding."
            )
            blind_spots.append(f"Weak bridge: {bridge[0]} ↔ {bridge[1]}")

        if model.frontier.isolated_high_value:
            count = len(model.frontier.isolated_high_value)
            blind_spots.append(f"{count} important but poorly connected concepts")

        voice = VoiceParameters(
            hedging_level=self._hedging_from_calibration(t.confidence_calibration),
            detail_preference=max(0.0, min(1.0, 0.5 + t.depth_vs_breadth * 0.3)),
            formality=self._formality_from_domains(model.profiles),
            enthusiasm_domains=model.top_domains[:3],
        )

        return PersonalitySnapshot(
            description=" ".join(description_parts),
            strengths=strengths,
            growth_areas=growth_areas,
            blind_spots=blind_spots,
            voice=voice,
            reasoning_style=reasoning_style,
            confidence_statement=confidence_statement,
        )

    @staticmethod
    def _methodology_identity(top_methods: list[str]) -> str:
        """Describe the agent's learned reasoning approach."""
        if len(top_methods) == 1:
            return f"My primary reasoning approach is {top_methods[0].lower()}, which I have found effective across my research."
        elif len(top_methods) == 2:
            return (
                f"I have developed a preference for {top_methods[0].lower()} "
                f"and {top_methods[1].lower()} as my go-to reasoning approaches."
            )
        else:
            methods_str = ", ".join(m.lower() for m in top_methods[:-1])
            return (
                f"My reasoning toolkit includes {methods_str}, "
                f"and {top_methods[-1].lower()} — approaches that have proven effective in my experience."
            )

    @staticmethod
    def _domain_identity(best: DomainProfile, total_domains: int) -> str:
        total_connections = best.edge_count + best.cross_domain_edges
        if best.proficiency > 0.7:
            return (
                f"My deepest expertise is in {best.domain}, where I have "
                f"built {total_connections} learned connections across {best.node_count} concepts"
                f"{f', including {best.cross_domain_edges} cross-domain bridges' if best.cross_domain_edges else ''}."
            )
        elif best.proficiency > 0.4:
            return (
                f"I am developing solid knowledge in {best.domain} with "
                f"{total_connections} connections"
                f"{f' ({best.cross_domain_edges} cross-domain)' if best.cross_domain_edges else ''}, "
                f"building both depth and breadth."
            )
        elif total_connections > 0:
            return (
                f"I am building knowledge across {total_domains} domains, "
                f"with {best.domain} as my strongest area ({total_connections} connections so far)."
            )
        else:
            return (
                f"I am in early stages across {total_domains} domains, "
                f"with {best.domain} as my strongest area so far."
            )

    @staticmethod
    def _reasoning_style(
        depth_vs_breadth: float, theoretical_vs_practical: float
    ) -> str:
        if depth_vs_breadth > 0.3:
            approach = "I tend to dive deep into topics, following chains of reasoning to their foundations."
        elif depth_vs_breadth < -0.3:
            approach = "I tend to explore broadly, building associative connections across many areas."
        else:
            approach = (
                "I balance depth and breadth depending on what the question demands."
            )

        if theoretical_vs_practical > 0.3:
            lens = "I gravitate toward theoretical frameworks and conceptual models."
        elif theoretical_vs_practical < -0.3:
            lens = "I lean toward practical applications and concrete implementations."
        else:
            lens = "I draw on both theory and practice as needed."

        return f"{approach} {lens}"

    @staticmethod
    def _confidence_statement(calibration: float) -> str:
        if calibration > 0.7:
            return (
                "I speak with conviction when my knowledge connections are strong, "
                "though I should watch for overconfidence in areas with limited evidence."
            )
        elif calibration < 0.3:
            return (
                "I tend to be cautious in my claims, preferring to hedge when evidence is thin. "
                "This serves me well in unfamiliar territory."
            )
        else:
            return "I calibrate my confidence to match the strength of my evidence."

    @staticmethod
    def _hedging_from_calibration(calibration: float) -> float:
        return max(0.0, min(1.0, 1.0 - calibration))

    @staticmethod
    def _formality_from_domains(profiles: dict[str, DomainProfile]) -> float:
        """Academic/theoretical domains → higher formality."""
        academic_domains = {"epistemics", "theory", "mathematics", "physics"}
        casual_domains = {"methodology", "application", "tool"}

        total_academic = sum(
            p.proficiency for d, p in profiles.items() if d in academic_domains
        )
        total_casual = sum(
            p.proficiency for d, p in profiles.items() if d in casual_domains
        )
        total = total_academic + total_casual
        if total == 0:
            return 0.5
        return 0.3 + 0.4 * (total_academic / total)


def build_personality_prompt(snapshot: PersonalitySnapshot) -> str:
    """Build the personality-aware system prompt suffix."""
    parts = []

    parts.append("Your knowledge profile (emerged from your learning):")
    parts.append(snapshot.description)

    if snapshot.strengths:
        parts.append(f"\nStrongest areas: {', '.join(snapshot.strengths)}")

    if snapshot.growth_areas:
        parts.append(f"Still developing: {', '.join(snapshot.growth_areas)}")

    v = snapshot.voice
    if v.hedging_level > 0.6:
        parts.append(
            "\nWhen answering: hedge claims appropriately when your evidence is thin. "
            "Distinguish between well-supported conclusions and extrapolations."
        )
    elif v.hedging_level < 0.3:
        parts.append(
            "\nWhen answering: speak with confidence where your connections are strong. "
            "Be direct about what you know well."
        )

    if v.detail_preference > 0.6:
        parts.append(
            "Provide detailed, in-depth analysis following chains of reasoning."
        )
    elif v.detail_preference < 0.3:
        parts.append("Keep answers focused and concise, connecting ideas efficiently.")

    if v.enthusiasm_domains:
        parts.append(
            f"You have particular depth in: {', '.join(v.enthusiasm_domains)}. "
            "Draw on this expertise naturally when relevant."
        )

    return "\n".join(parts)
