import setuptools

with open("README.md", "r", encoding="utf-8") as fh:
    long_description = fh.read()

setuptools.setup(
    name="nexudus_doorflow_sync", # Replace with your own username
    version="0.0.1",
    author="Ben Brown",
    author_email="brown@lowellmakes.com",
    description="Sync DoorFlow and Nexudus access controll",
    long_description=long_description,
    long_description_content_type="text/markdown",
    url="https://github.com/lowellmakes-it/nexudus-doorflow-sync",
    packages=setuptools.find_packages(),
    classifiers=[
        "Programming Language :: Python :: 3",
        "License :: OSI Approved :: MIT License",
        "Operating System :: OS Independent",
    ],
    python_requires='>=3.6',
)
