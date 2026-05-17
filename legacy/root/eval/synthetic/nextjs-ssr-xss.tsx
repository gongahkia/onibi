type PageProps = {
  searchParams: {
    preview?: string;
  };
};

export default function Page({ searchParams }: PageProps) {
  const preview = searchParams.preview ?? "";

  return <section dangerouslySetInnerHTML={{ __html: preview }} />;
}
