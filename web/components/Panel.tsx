/** A narrow centred box for sign-in style pages. */
export default function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mx-auto mt-10 max-w-[420px]">
      <h1 className="text-[26px] font-bold">{title}</h1>
      {children}
    </div>
  );
}
