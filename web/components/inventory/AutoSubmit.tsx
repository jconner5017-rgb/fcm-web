"use client";

/** A select that submits its form as soon as it changes. */
export default function AutoSubmit(props: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return <select {...props} onChange={(e) => e.currentTarget.form?.requestSubmit()} />;
}
