"""RFC document writer workflow skill.

Generates an RFC document by gathering context, drafting sections with
the LLM, and writing the final document to disk.
"""

def write_rfc(input):
    """Write an RFC document for the given title and context.

    Args:
        input: A dict with keys "title" and "context".

    Returns:
        The path to the written RFC file.
    """
    title = input["title"]
    context = input["context"]

    prompt = "Write an RFC document titled '{}'. Context: {}".format(title, context)
    prompt += "\n\nInclude these sections: Summary, Motivation, Detailed Design, Alternatives Considered, and Open Questions."

    draft = llm_complete(prompt)

    filename = "rfc-{}.md".format(title.lower().replace(" ", "-"))
    write_file(filename, draft)

    return filename

register_workflow(
    name = "write_rfc",
    handler = write_rfc,
)
