"""RFC document writer workflow skill.

Generates an RFC document by gathering context, drafting sections with
the LLM, and writing the final document to disk.
"""

def write_rfc(title, context):
    """Write an RFC document for the given title and context.

    Args:
        title: The title of the RFC.
        context: Background information and requirements for the RFC.

    Returns:
        The path to the written RFC file.
    """
    prompt = "Write an RFC document titled '{}'. Context: {}".format(title, context)
    prompt += "\n\nInclude these sections: Summary, Motivation, Detailed Design, Alternatives Considered, and Open Questions."

    draft = llm_call(prompt)

    filename = "rfc-{}.md".format(title.lower().replace(" ", "-"))
    file_write(filename, draft)

    return filename

register_workflow(
    name = "write_rfc",
    description = "Generate an RFC document from a title and context description",
    fn = write_rfc,
)
