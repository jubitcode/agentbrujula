import type { CSSProperties } from "react";

type Props = {
  size?: number | string;
  style?: CSSProperties;
  className?: string;
  title?: string;
  // kept for backward-compat with existing callsites; ignored for this design
  color?: string;
  nodeColor?: string;
};

export function BrujulaLogo({
  size = 400,
  style,
  className,
  title = "Brújula",
}: Props) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      viewBox="0 0 400 400"
      role="img"
      aria-label={title}
      className={className}
      style={style}
    >
      <title>{title}</title>
      <defs>
        <style>{`@import url('https://fonts.googleapis.com/css2?family=Cinzel:wght@400;600;700&display=swap');`}</style>
      </defs>
      <circle cx="200" cy="200" r="177" fill="none" stroke="#1A4A72" strokeWidth="0.5" opacity="0.35" />
      <circle cx="200" cy="200" r="173" fill="#0F2035" stroke="#4A9ED6" strokeWidth="2.5" />
      <circle cx="200" cy="200" r="140" fill="#071220" />
      <circle cx="200" cy="200" r="140" fill="none" stroke="#4A9ED6" strokeWidth="2.2" />
      <circle cx="200" cy="200" r="139" fill="none" stroke="#90CEF0" strokeWidth="0.6" opacity="0.3" />
      <circle cx="200" cy="200" r="113" fill="#040C18" />
      <circle cx="200" cy="200" r="113" fill="none" stroke="#1A4A72" strokeWidth="1.0" opacity="0.6" />
      <circle cx="200" cy="200" r="83" fill="none" stroke="#1A4A72" strokeWidth="0.7" opacity="0.4" />
      <line x1="200" y1="27" x2="200" y2="51" stroke="#1A4A72" strokeWidth="2" />
      <line x1="230" y1="29.6" x2="228.7" y2="37.5" stroke="#10243A" strokeWidth="0.7" />
      <line x1="259.2" y1="37.4" x2="256.4" y2="45" stroke="#10243A" strokeWidth="0.7" />
      <line x1="286.5" y1="50.2" x2="282.5" y2="57.1" stroke="#10243A" strokeWidth="0.7" />
      <line x1="311.2" y1="67.5" x2="306.1" y2="73.6" stroke="#10243A" strokeWidth="0.7" />
      <line x1="332.5" y1="88.8" x2="326.4" y2="93.9" stroke="#10243A" strokeWidth="0.7" />
      <line x1="349.8" y1="113.5" x2="342.9" y2="117.5" stroke="#10243A" strokeWidth="0.7" />
      <line x1="362.6" y1="140.8" x2="355" y2="143.6" stroke="#10243A" strokeWidth="0.7" />
      <line x1="370.4" y1="170" x2="362.5" y2="171.3" stroke="#10243A" strokeWidth="0.7" />
      <line x1="373" y1="200" x2="349" y2="200" stroke="#1A4A72" strokeWidth="2" />
      <line x1="370.4" y1="230" x2="362.5" y2="228.7" stroke="#10243A" strokeWidth="0.7" />
      <line x1="362.6" y1="259.2" x2="355" y2="256.4" stroke="#10243A" strokeWidth="0.7" />
      <line x1="349.8" y1="286.5" x2="342.9" y2="282.5" stroke="#10243A" strokeWidth="0.7" />
      <line x1="332.5" y1="311.2" x2="326.4" y2="306.1" stroke="#10243A" strokeWidth="0.7" />
      <line x1="311.2" y1="332.5" x2="306.1" y2="326.4" stroke="#10243A" strokeWidth="0.7" />
      <line x1="286.5" y1="349.8" x2="282.5" y2="342.9" stroke="#10243A" strokeWidth="0.7" />
      <line x1="259.2" y1="362.6" x2="256.4" y2="355" stroke="#10243A" strokeWidth="0.7" />
      <line x1="230" y1="370.4" x2="228.7" y2="362.5" stroke="#10243A" strokeWidth="0.7" />
      <line x1="200" y1="373" x2="200" y2="349" stroke="#1A4A72" strokeWidth="2" />
      <line x1="170" y1="370.4" x2="171.3" y2="362.5" stroke="#10243A" strokeWidth="0.7" />
      <line x1="140.8" y1="362.6" x2="143.6" y2="355" stroke="#10243A" strokeWidth="0.7" />
      <line x1="113.5" y1="349.8" x2="117.5" y2="342.9" stroke="#10243A" strokeWidth="0.7" />
      <line x1="88.8" y1="332.5" x2="93.9" y2="326.4" stroke="#10243A" strokeWidth="0.7" />
      <line x1="67.5" y1="311.2" x2="73.6" y2="306.1" stroke="#10243A" strokeWidth="0.7" />
      <line x1="50.2" y1="286.5" x2="57.1" y2="282.5" stroke="#10243A" strokeWidth="0.7" />
      <line x1="37.4" y1="259.2" x2="45" y2="256.4" stroke="#10243A" strokeWidth="0.7" />
      <line x1="29.6" y1="230" x2="37.5" y2="228.7" stroke="#10243A" strokeWidth="0.7" />
      <line x1="27" y1="200" x2="51" y2="200" stroke="#1A4A72" strokeWidth="2" />
      <line x1="29.6" y1="170" x2="37.5" y2="171.3" stroke="#10243A" strokeWidth="0.7" />
      <line x1="37.4" y1="140.8" x2="45" y2="143.6" stroke="#10243A" strokeWidth="0.7" />
      <line x1="50.2" y1="113.5" x2="57.1" y2="117.5" stroke="#10243A" strokeWidth="0.7" />
      <line x1="67.5" y1="88.8" x2="73.6" y2="93.9" stroke="#10243A" strokeWidth="0.7" />
      <line x1="88.8" y1="67.5" x2="93.9" y2="73.6" stroke="#10243A" strokeWidth="0.7" />
      <line x1="113.5" y1="50.2" x2="117.5" y2="57.1" stroke="#10243A" strokeWidth="0.7" />
      <line x1="140.8" y1="37.4" x2="143.6" y2="45" stroke="#10243A" strokeWidth="0.7" />
      <line x1="170" y1="29.6" x2="171.3" y2="37.5" stroke="#10243A" strokeWidth="0.7" />
      <line x1="208" y1="27" x2="192" y2="27" stroke="#4A9ED6" strokeWidth="2" strokeLinecap="round" />
      <line x1="205" y1="37" x2="195" y2="37" stroke="#4A9ED6" strokeWidth="0.9" strokeLinecap="round" opacity="0.45" />
      <line x1="373" y1="208" x2="373" y2="192" stroke="#4A9ED6" strokeWidth="2" strokeLinecap="round" />
      <line x1="363" y1="205" x2="363" y2="195" stroke="#4A9ED6" strokeWidth="0.9" strokeLinecap="round" opacity="0.45" />
      <line x1="192" y1="373" x2="208" y2="373" stroke="#4A9ED6" strokeWidth="2" strokeLinecap="round" />
      <line x1="195" y1="363" x2="205" y2="363" stroke="#4A9ED6" strokeWidth="0.9" strokeLinecap="round" opacity="0.45" />
      <line x1="27" y1="192" x2="27" y2="208" stroke="#4A9ED6" strokeWidth="2" strokeLinecap="round" />
      <line x1="37" y1="195" x2="37" y2="205" stroke="#4A9ED6" strokeWidth="0.9" strokeLinecap="round" opacity="0.45" />
      <polygon points="200,57 180,184 200,207" fill="#0F2E55" />
      <polygon points="200,57 200,207 220,184" fill="#1F558A" />
      <polygon points="200,57 203.5,132 200,207 196.5,132" fill="#90CEF0" />
      <polygon points="200,343 182,214 200,193" fill="#1F558A" />
      <polygon points="200,343 200,193 218,214" fill="#0F2E55" />
      <polygon points="200,343 196.5,268 200,193 203.5,268" fill="#3B7AB8" />
      <polygon points="343,200 252,180 193,200" fill="#0F2E55" />
      <polygon points="343,200 193,200 252,220" fill="#1F558A" />
      <polygon points="343,200 268,203.5 193,200 268,196.5" fill="#3B7AB8" />
      <polygon points="57,200 148,220 207,200" fill="#1F558A" />
      <polygon points="57,200 207,200 148,180" fill="#0F2E55" />
      <polygon points="57,200 132,196.5 207,200 132,203.5" fill="#3B7AB8" />
      <polygon points="200,140 252,170 252,230 200,260 148,230 148,170" fill="none" stroke="#1A4A72" strokeWidth="0.8" opacity="0.5" />
      <line x1="200" y1="200" x2="200" y2="140" stroke="#1A4A72" strokeWidth="0.6" opacity="0.4" />
      <circle cx="200" cy="140" r="5.5" fill="#071828" />
      <circle cx="200" cy="140" r="5.5" fill="none" stroke="#1A4A72" strokeWidth="1" opacity="0.5" />
      <circle cx="200" cy="140" r="1.8" fill="#1A4A72" opacity="0.6" />
      <line x1="200" y1="200" x2="252" y2="170" stroke="#1A4A72" strokeWidth="0.6" opacity="0.4" />
      <circle cx="252" cy="170" r="5.5" fill="#071828" />
      <circle cx="252" cy="170" r="5.5" fill="none" stroke="#1A4A72" strokeWidth="1" opacity="0.5" />
      <circle cx="252" cy="170" r="1.8" fill="#1A4A72" opacity="0.6" />
      <line x1="200" y1="200" x2="252" y2="230" stroke="#1A4A72" strokeWidth="0.6" opacity="0.4" />
      <circle cx="252" cy="230" r="5.5" fill="#071828" />
      <circle cx="252" cy="230" r="5.5" fill="none" stroke="#1A4A72" strokeWidth="1" opacity="0.5" />
      <circle cx="252" cy="230" r="1.8" fill="#1A4A72" opacity="0.6" />
      <line x1="200" y1="200" x2="200" y2="260" stroke="#1A4A72" strokeWidth="0.6" opacity="0.4" />
      <circle cx="200" cy="260" r="5.5" fill="#071828" />
      <circle cx="200" cy="260" r="5.5" fill="none" stroke="#1A4A72" strokeWidth="1" opacity="0.5" />
      <circle cx="200" cy="260" r="1.8" fill="#1A4A72" opacity="0.6" />
      <line x1="200" y1="200" x2="148" y2="230" stroke="#1A4A72" strokeWidth="0.6" opacity="0.4" />
      <circle cx="148" cy="230" r="5.5" fill="#071828" />
      <circle cx="148" cy="230" r="5.5" fill="none" stroke="#1A4A72" strokeWidth="1" opacity="0.5" />
      <circle cx="148" cy="230" r="1.8" fill="#1A4A72" opacity="0.6" />
      <line x1="200" y1="200" x2="148" y2="170" stroke="#1A4A72" strokeWidth="0.6" opacity="0.4" />
      <circle cx="148" cy="170" r="5.5" fill="#071828" />
      <circle cx="148" cy="170" r="5.5" fill="none" stroke="#1A4A72" strokeWidth="1" opacity="0.5" />
      <circle cx="148" cy="170" r="1.8" fill="#1A4A72" opacity="0.6" />
      <polygon points="200,200 200,178 208.5,191.5" fill="#3B7AB8" />
      <polygon points="200,200 208.5,191.5 222,200" fill="#0F2E55" />
      <polygon points="200,200 222,200 208.5,208.5" fill="#3B7AB8" />
      <polygon points="200,200 208.5,208.5 200,222" fill="#0F2E55" />
      <polygon points="200,200 200,222 191.5,208.5" fill="#3B7AB8" />
      <polygon points="200,200 191.5,208.5 178,200" fill="#0F2E55" />
      <polygon points="200,200 178,200 191.5,191.5" fill="#3B7AB8" />
      <polygon points="200,200 191.5,191.5 200,178" fill="#0F2E55" />
      <polygon points="200,178 222,200 200,222 178,200" fill="#90CEF0" opacity="0.55" />
      <circle cx="200" cy="200" r="4.5" fill="#90CEF0" />
      <text x="200" y="42" textAnchor="middle" dominantBaseline="middle" fontFamily="Cinzel, serif" fontSize="14" fontWeight="600" fill="#FFFFFF">N</text>
      <text x="358" y="200" textAnchor="middle" dominantBaseline="middle" fontFamily="Cinzel, serif" fontSize="12" fontWeight="400" fill="#FFFFFF">E</text>
      <text x="200" y="358" textAnchor="middle" dominantBaseline="middle" fontFamily="Cinzel, serif" fontSize="14" fontWeight="600" fill="#FFFFFF">S</text>
      <text x="42" y="200" textAnchor="middle" dominantBaseline="middle" fontFamily="Cinzel, serif" fontSize="12" fontWeight="400" fill="#FFFFFF">W</text>
      <text x="200" y="202" textAnchor="middle" dominantBaseline="middle" fontFamily="Cinzel, serif" fontSize="22" fontWeight="700" fill="#FFFFFF">B</text>
    </svg>
  );
}
